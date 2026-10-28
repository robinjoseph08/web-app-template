// Package devtool implements the local development commands behind the mise
// tasks: a shared PostgreSQL container with a database per worktree, and dev
// servers on ports that don't collide between worktrees.
package devtool

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

const (
	defaultAPIPort = 3579
	defaultWebPort = 5173
	// The e2e server starts from its own port, so a running dev API keeps its
	// usual one.
	defaultE2EPort = 4579

	startAll = "all"
	startAir = "air"
	startAPI = "api"
	startWeb = "web"

	// testDatabaseURLEnv points the Go tests at an existing database, as CI
	// does, instead of the shared Docker container.
	testDatabaseURLEnv = "TEST_DATABASE_URL"

	// e2eAll runs every browser in playwright.config.ts.
	e2eAll = "all"
)

// e2eBrowsers are the Playwright projects in playwright.config.ts.
var e2eBrowsers = []string{"chromium", "firefox", "webkit"}

// defaultTestArgs bound test parallelism: each top-level database test
// migrates its own schema, and Go runs one parallel test per core by default,
// which can exhaust PostgreSQL's connections on a machine with many cores.
var defaultTestArgs = []string{"./...", "-parallel", "4", "-coverprofile", "coverage.out"}

// App implements the command interface used by mise tasks.
type App struct {
	Stdin     io.Reader
	Stdout    io.Writer
	Resolve   func(context.Context) (Environment, error)
	Database  DatabaseManager
	Ports     func(Environment) PortAllocator
	Processes ProcessManager
	Getenv    func(string) string
}

func NewApp() *App {
	return &App{
		Stdin:    os.Stdin,
		Stdout:   os.Stdout,
		Resolve:  ResolveEnvironment,
		Database: &DockerDatabase{Stdout: os.Stdout},
		Ports: func(env Environment) PortAllocator {
			return &FilePortAllocator{LockRoot: portLockRoot(env), Owner: env.CurrentRoot}
		},
		Processes: &OSProcesses{},
		Getenv:    os.Getenv,
	}
}

func (a *App) Run(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return errors.New("command is required")
	}

	env, err := a.Resolve(ctx)
	if err != nil {
		return fmt.Errorf("resolve development environment: %w", err)
	}

	switch args[0] {
	case "setup":
		return a.setup(ctx, env)
	case "start":
		return a.start(ctx, env, args[1:])
	case "db":
		return a.database(ctx, env, args[1:])
	case "clone":
		return a.clone(ctx, env, args[1:])
	case "test":
		return a.test(ctx, env, args[1:])
	case "psql":
		return a.psql(ctx, env, args[1:])
	case "e2e":
		return a.e2e(ctx, env, args[1:])
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func (a *App) setup(ctx context.Context, env Environment) error {
	port, err := a.Database.Ensure(ctx, env)
	if err != nil {
		return err
	}
	if err := a.ensureDevelopmentDatabase(ctx, env); err != nil {
		return err
	}
	if err := a.createIfMissing(ctx, env, env.TestDatabase()); err != nil {
		return err
	}
	return a.printf("Ready. Databases %s and %s are on PostgreSQL port %d.\n", env.CurrentDatabase, env.TestDatabase(), port)
}

func (a *App) start(ctx context.Context, env Environment, args []string) error {
	mode := startAll
	if len(args) > 1 {
		return errors.New("start accepts at most one mode")
	}
	if len(args) == 1 {
		mode = args[0]
	}
	switch mode {
	case startAll, startAir, startAPI, startWeb:
	default:
		return fmt.Errorf("unknown start mode %q", mode)
	}

	servers := Servers{CookieNamespace: env.CookieNamespace()}
	runsAPI := mode != startWeb
	runsWeb := mode == startAll || mode == startWeb
	ports := a.Ports(env)

	if runsAPI {
		port, err := a.Database.Ensure(ctx, env)
		if err != nil {
			return err
		}
		if err := a.ensureDevelopmentDatabase(ctx, env); err != nil {
			return err
		}
		servers.DatabaseURL = DatabaseURL(port, env.CurrentDatabase)

		lease, err := ports.Acquire(defaultAPIPort)
		if err != nil {
			return fmt.Errorf("reserve API port: %w", err)
		}
		defer func() { _ = lease.Close() }()
		servers.APIPort = lease.Port()
		servers.APIURL = serverURL(servers.APIPort)
		forget, err := recordAPIPort(env, servers.APIPort)
		if err != nil {
			return err
		}
		defer forget()
	}

	if runsWeb {
		if !runsAPI {
			// The frontend alone talks to the API this worktree is already
			// running. Guessing a port could reach another worktree's API,
			// and with it that worktree's database.
			port, err := recordedAPIPort(env)
			if err != nil {
				return err
			}
			if port == 0 {
				return errors.New("no API is running in this worktree; start one with mise start:air, or run both with mise start")
			}
			servers.APIURL = serverURL(port)
		}
		lease, err := ports.Acquire(defaultWebPort)
		if err != nil {
			return fmt.Errorf("reserve web port: %w", err)
		}
		defer func() { _ = lease.Close() }()
		servers.WebPort = lease.Port()
	}

	if runsAPI {
		if err := a.printf("Database: %s\nAPI: http://localhost:%d\n", env.CurrentDatabase, servers.APIPort); err != nil {
			return err
		}
	}
	if runsWeb {
		if err := a.printf("Web: http://localhost:%d (API: %s)\n", servers.WebPort, servers.APIURL); err != nil {
			return err
		}
	}
	return a.Processes.Start(ctx, env, mode, servers)
}

func (a *App) database(ctx context.Context, env Environment, args []string) error {
	if len(args) != 1 {
		return errors.New("database command must be migrate, rollback, reset, or stop")
	}
	if args[0] == "stop" {
		return a.Database.Stop(ctx, env)
	}
	if _, err := a.Database.Ensure(ctx, env); err != nil {
		return err
	}

	switch args[0] {
	case "migrate":
		return a.ensureDevelopmentDatabase(ctx, env)
	case "rollback":
		return a.Database.Rollback(ctx, env, env.CurrentDatabase)
	case "reset":
		exists, err := a.Database.Exists(ctx, env, env.CurrentDatabase)
		if err != nil {
			return err
		}
		if exists {
			confirmed, err := a.confirm(ctx, fmt.Sprintf("Reset database %s and delete all of its data?", env.CurrentDatabase))
			if err != nil {
				return err
			}
			if !confirmed {
				return a.printf("Reset cancelled.\n")
			}
		}
		if err := a.Database.Reset(ctx, env, env.CurrentDatabase); err != nil {
			return err
		}
		return a.Database.Migrate(ctx, env, env.CurrentDatabase)
	default:
		return fmt.Errorf("unknown database command %q", args[0])
	}
}

func (a *App) clone(ctx context.Context, env Environment, args []string) error {
	toMain := false
	for _, arg := range args {
		if arg != "--to-main" {
			return fmt.Errorf("unknown clone argument %q", arg)
		}
		toMain = true
	}
	if env.IsMain() {
		return errors.New("clone must be run from a linked worktree")
	}
	if _, err := a.Database.Ensure(ctx, env); err != nil {
		return err
	}

	source, target := env.MainDatabase, env.CurrentDatabase
	sourceLabel, targetLabel := "main", filepath.Base(env.CurrentRoot)
	if toMain {
		source, target = target, source
		sourceLabel, targetLabel = targetLabel, sourceLabel
	}
	sourceExists, err := a.Database.Exists(ctx, env, source)
	if err != nil {
		return err
	}
	if !sourceExists {
		return fmt.Errorf("source database %s does not exist", source)
	}
	targetExists, err := a.Database.Exists(ctx, env, target)
	if err != nil {
		return err
	}
	if targetExists {
		confirmed, err := a.confirm(ctx, fmt.Sprintf("Replace %s data with data from %s?", targetLabel, sourceLabel))
		if err != nil {
			return err
		}
		if !confirmed {
			return a.printf("Clone cancelled.\n")
		}
	}
	if err := a.Database.Clone(ctx, env, source, target); err != nil {
		return err
	}
	return a.printf("Cloned database from %s to %s.\n", sourceLabel, targetLabel)
}

// test runs the Go tests against this worktree's test database, or against
// the one TEST_DATABASE_URL names when that's set, as in CI.
func (a *App) test(ctx context.Context, env Environment, args []string) error {
	if len(args) == 0 {
		args = defaultTestArgs
	}
	if url := a.Getenv(testDatabaseURLEnv); url != "" {
		return a.Processes.Test(ctx, env, url, args...)
	}
	port, err := a.Database.Ensure(ctx, env)
	if err != nil {
		return err
	}
	if err := a.createIfMissing(ctx, env, env.TestDatabase()); err != nil {
		return err
	}
	return a.Processes.Test(ctx, env, DatabaseURL(port, env.TestDatabase()), args...)
}

func (a *App) psql(ctx context.Context, env Environment, args []string) error {
	test := false
	for _, arg := range args {
		if arg != "--test" {
			return fmt.Errorf("unknown psql argument %q", arg)
		}
		test = true
	}
	if _, err := a.Database.Ensure(ctx, env); err != nil {
		return err
	}
	// Only a database this creates gets set up, so psql shows an existing
	// database as it is, for example right after a rollback.
	if test {
		if err := a.createIfMissing(ctx, env, env.TestDatabase()); err != nil {
			return err
		}
		return a.Database.Psql(ctx, env, env.TestDatabase())
	}
	exists, err := a.Database.Exists(ctx, env, env.CurrentDatabase)
	if err != nil {
		return err
	}
	if !exists {
		if err := a.ensureDevelopmentDatabase(ctx, env); err != nil {
			return err
		}
	}
	return a.Database.Psql(ctx, env, env.CurrentDatabase)
}

// e2e runs the Playwright tests against the production build of the site,
// served by the binary, the way it's deployed. The binary uses its own
// database, rebuilt on every run, so tests start from the same state and never
// touch the development database. An optional browser comes first; any other
// arguments go to Playwright, like a spec file or --headed.
func (a *App) e2e(ctx context.Context, env Environment, args []string) error {
	var playwrightArgs []string
	if len(args) > 0 && (args[0] == e2eAll || slices.Contains(e2eBrowsers, args[0])) {
		if args[0] != e2eAll {
			playwrightArgs = append(playwrightArgs, "--project="+args[0])
		}
		args = args[1:]
	}
	playwrightArgs = append(playwrightArgs, args...)

	// A second run in this worktree would rebuild the database and the
	// frontend under the first.
	unlock, err := lockFile(ctx, filepath.Join(env.CurrentRoot, "tmp", "e2e.lock"), func() {
		_ = a.printf("Waiting for another e2e run in this worktree to finish...\n")
	})
	if err != nil {
		return err
	}
	defer unlock()

	port, err := a.Database.Ensure(ctx, env)
	if err != nil {
		return err
	}
	name := env.E2EDatabase()
	if err := a.printf("Rebuilding database %s...\n", name); err != nil {
		return err
	}
	if err := a.Database.Reset(ctx, env, name); err != nil {
		return err
	}
	if err := a.Database.Migrate(ctx, env, name); err != nil {
		return err
	}

	lease, err := a.Ports(env).Acquire(defaultE2EPort)
	if err != nil {
		return fmt.Errorf("reserve e2e server port: %w", err)
	}
	defer func() { _ = lease.Close() }()
	logPath, err := filepath.Rel(env.CurrentRoot, e2eServerLog(env))
	if err != nil {
		return err
	}
	if err := a.printf("E2E server: %s (log in %s)\n", serverURL(lease.Port()), logPath); err != nil {
		return err
	}
	return a.Processes.E2E(ctx, env, DatabaseURL(port, name), lease.Port(), playwrightArgs...)
}

// ensureDevelopmentDatabase creates this worktree's database if needed, copied
// from the main worktree's when that exists, and brings it up to date.
func (a *App) ensureDevelopmentDatabase(ctx context.Context, env Environment) error {
	exists, err := a.Database.Exists(ctx, env, env.CurrentDatabase)
	if err != nil {
		return err
	}
	if !exists {
		mainExists := false
		if !env.IsMain() {
			if mainExists, err = a.Database.Exists(ctx, env, env.MainDatabase); err != nil {
				return err
			}
		}
		if mainExists {
			if err := a.printf("Copying the main worktree's database into %s...\n", env.CurrentDatabase); err != nil {
				return err
			}
			if err := a.Database.Clone(ctx, env, env.MainDatabase, env.CurrentDatabase); err != nil {
				return fmt.Errorf("clone main database: %w", err)
			}
		} else if err := a.Database.Create(ctx, env, env.CurrentDatabase); err != nil {
			return err
		}
	}
	return a.Database.Migrate(ctx, env, env.CurrentDatabase)
}

// createIfMissing creates an empty database if it doesn't exist yet.
func (a *App) createIfMissing(ctx context.Context, env Environment, name string) error {
	exists, err := a.Database.Exists(ctx, env, name)
	if err != nil || exists {
		return err
	}
	return a.Database.Create(ctx, env, name)
}

func apiPortPath(env Environment) string {
	return filepath.Join(env.CurrentRoot, "tmp", "api-port")
}

// recordAPIPort leaves this worktree's API port where a separately started
// frontend can find it. Ports are chosen at start, so there is nowhere else to
// look it up.
func recordAPIPort(env Environment, port int) (func(), error) {
	path := apiPortPath(env)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create %s: %w", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(strconv.Itoa(port)), 0o600); err != nil {
		return nil, fmt.Errorf("record API port: %w", err)
	}
	return func() { _ = os.Remove(path) }, nil
}

// recordedAPIPort returns the port of the API running in this worktree, or
// zero if none is. A recorded port this worktree no longer holds the lease on
// was left behind by a launcher that didn't exit cleanly, so it's ignored.
func recordedAPIPort(env Environment) (int, error) {
	contents, err := os.ReadFile(apiPortPath(env))
	if errors.Is(err, os.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("read API port: %w", err)
	}
	port, err := strconv.Atoi(strings.TrimSpace(string(contents)))
	if err != nil {
		return 0, fmt.Errorf("read API port: %w", err)
	}
	if !portLeasedBy(portLockRoot(env), port, env.CurrentRoot) {
		return 0, nil
	}
	return port, nil
}

func portLockRoot(env Environment) string {
	return filepath.Join(env.MainRoot, "tmp", "ports")
}

// confirm asks a yes/no question. Ctrl+C while it waits for an answer stops
// the command instead of waiting for Enter.
func (a *App) confirm(ctx context.Context, message string) (bool, error) {
	if err := a.printf("%s [y/N] ", message); err != nil {
		return false, err
	}
	type answer struct {
		line string
		err  error
	}
	answers := make(chan answer, 1)
	go func() {
		line, err := bufio.NewReader(a.Stdin).ReadString('\n')
		answers <- answer{line: line, err: err}
	}()
	select {
	case <-ctx.Done():
		return false, ctx.Err()
	case got := <-answers:
		if got.err != nil && !errors.Is(got.err, io.EOF) {
			return false, fmt.Errorf("read confirmation: %w", got.err)
		}
		reply := strings.ToLower(strings.TrimSpace(got.line))
		return reply == "y" || reply == "yes", nil
	}
}

func (a *App) printf(format string, values ...any) error {
	if _, err := fmt.Fprintf(a.Stdout, format, values...); err != nil {
		return fmt.Errorf("write command output: %w", err)
	}
	return nil
}
