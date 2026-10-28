package devtool

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeDatabase struct {
	port      int
	databases map[string]bool
	calls     []string
}

func (f *fakeDatabase) Ensure(context.Context, Environment) (int, error) {
	f.calls = append(f.calls, "ensure")
	return f.port, nil
}

func (f *fakeDatabase) Stop(context.Context, Environment) error {
	f.calls = append(f.calls, "stop")
	return nil
}

func (f *fakeDatabase) Exists(_ context.Context, _ Environment, name string) (bool, error) {
	return f.databases[name], nil
}

func (f *fakeDatabase) Create(_ context.Context, _ Environment, name string) error {
	f.calls = append(f.calls, "create "+name)
	f.databases[name] = true
	return nil
}

func (f *fakeDatabase) Clone(_ context.Context, _ Environment, source, target string) error {
	f.calls = append(f.calls, "clone "+source+" "+target)
	f.databases[target] = true
	return nil
}

func (f *fakeDatabase) Reset(_ context.Context, _ Environment, name string) error {
	f.calls = append(f.calls, "reset "+name)
	return nil
}

func (f *fakeDatabase) Migrate(_ context.Context, _ Environment, name string) error {
	f.calls = append(f.calls, "migrate "+name)
	return nil
}

func (f *fakeDatabase) Rollback(_ context.Context, _ Environment, name string) error {
	f.calls = append(f.calls, "rollback "+name)
	return nil
}

func (f *fakeDatabase) Psql(_ context.Context, _ Environment, name string) error {
	f.calls = append(f.calls, "psql "+name)
	return nil
}

type fakeProcesses struct {
	calls   []string
	servers Servers
	mode    string
}

func (f *fakeProcesses) Start(_ context.Context, _ Environment, mode string, servers Servers) error {
	f.mode = mode
	f.servers = servers
	return nil
}

func (f *fakeProcesses) Test(_ context.Context, _ Environment, databaseURL string, args ...string) error {
	f.calls = append(f.calls, fmt.Sprintf("test %s %s", databaseURL, strings.Join(args, " ")))
	return nil
}

func (f *fakeProcesses) E2E(_ context.Context, _ Environment, databaseURL string, port int, args ...string) error {
	f.calls = append(f.calls, strings.TrimSpace(fmt.Sprintf("e2e %s on %d %s", databaseURL, port, strings.Join(args, " "))))
	return nil
}

type fixedPorts struct{}

type fixedLease struct{ port int }

func (l fixedLease) Port() int    { return l.port }
func (l fixedLease) Close() error { return nil }

func (p *fixedPorts) Acquire(preferred int) (PortLease, error) {
	return fixedLease{port: preferred}, nil
}

type harness struct {
	app       *App
	env       Environment
	database  *fakeDatabase
	processes *fakeProcesses
	stdout    *bytes.Buffer
	getenv    map[string]string
}

func newHarness(t *testing.T, main bool) *harness {
	t.Helper()
	root := t.TempDir()
	env := Environment{
		MainRoot:        filepath.Join(root, "main"),
		CurrentRoot:     filepath.Join(root, "worktree"),
		MainDatabase:    "app",
		CurrentDatabase: "app_worktree",
		ProjectName:     "app_1234",
	}
	if main {
		env.CurrentRoot = env.MainRoot
		env.CurrentDatabase = env.MainDatabase
	}
	h := &harness{
		env:       env,
		database:  &fakeDatabase{port: 5544, databases: map[string]bool{}},
		processes: &fakeProcesses{},
		stdout:    &bytes.Buffer{},
		getenv:    map[string]string{},
	}
	h.app = &App{
		Stdin:     strings.NewReader(""),
		Stdout:    h.stdout,
		Resolve:   func(context.Context) (Environment, error) { return env, nil },
		Database:  h.database,
		Ports:     func(Environment) PortAllocator { return &fixedPorts{} },
		Processes: h.processes,
		Getenv:    func(key string) string { return h.getenv[key] },
	}
	return h
}

func TestSetupStartsPostgresFromAnyWorktree(t *testing.T) {
	t.Parallel()
	h := newHarness(t, false)
	h.database.databases["app"] = true

	require.NoError(t, h.app.Run(t.Context(), []string{"setup"}))

	assert.Equal(t, []string{"ensure", "clone app app_worktree", "migrate app_worktree", "create app_worktree_test"}, h.database.calls)
	assert.Contains(t, h.stdout.String(), "Databases app_worktree and app_worktree_test are on PostgreSQL port 5544")
}

func TestSetupPreservesExistingData(t *testing.T) {
	t.Parallel()
	h := newHarness(t, false)
	h.database.databases["app"] = true
	h.database.databases["app_worktree"] = true
	h.database.databases["app_worktree_test"] = true

	require.NoError(t, h.app.Run(t.Context(), []string{"setup"}))

	assert.Equal(t, []string{"ensure", "migrate app_worktree"}, h.database.calls)
}

func TestSetupCreatesTheMainDatabase(t *testing.T) {
	t.Parallel()
	h := newHarness(t, true)

	require.NoError(t, h.app.Run(t.Context(), []string{"setup"}))

	assert.Equal(t, []string{"ensure", "create app", "migrate app", "create app_test"}, h.database.calls)
}

func TestStartAllRunsBothServersAgainstTheWorktreeDatabase(t *testing.T) {
	t.Parallel()
	h := newHarness(t, false)
	h.database.databases["app_worktree"] = true

	require.NoError(t, h.app.Run(t.Context(), []string{"start"}))

	assert.Equal(t, startAll, h.processes.mode)
	assert.Equal(t, Servers{
		DatabaseURL:     "postgres://postgres:postgres@127.0.0.1:5544/app_worktree?sslmode=disable",
		APIPort:         defaultAPIPort,
		WebPort:         defaultWebPort,
		APIURL:          "http://127.0.0.1:3579",
		CookieNamespace: "app_worktree",
	}, h.processes.servers)
	assert.Equal(t, []string{"ensure", "migrate app_worktree"}, h.database.calls)
}

func TestStartCreatesAnEmptyDatabaseWithoutAMainDatabase(t *testing.T) {
	t.Parallel()
	h := newHarness(t, false)

	require.NoError(t, h.app.Run(t.Context(), []string{"start", "api"}))

	assert.Equal(t, []string{"ensure", "create app_worktree", "migrate app_worktree"}, h.database.calls)
}

func TestStartGivesEachWorktreeItsOwnCookies(t *testing.T) {
	t.Parallel()
	main := newHarness(t, true)
	require.NoError(t, main.app.Run(t.Context(), []string{"start", "air"}))

	linked := newHarness(t, false)
	require.NoError(t, linked.app.Run(t.Context(), []string{"start", "air"}))

	assert.Equal(t, "app", main.processes.servers.CookieNamespace)
	assert.Equal(t, "app_worktree", linked.processes.servers.CookieNamespace)
}

func TestStartWebRefusesToGuessAnAPI(t *testing.T) {
	t.Parallel()
	h := newHarness(t, false)

	err := h.app.Run(t.Context(), []string{"start", "web"})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "no API is running in this worktree")
	assert.Empty(t, h.database.calls)
	assert.Empty(t, h.processes.mode, "nothing is started")
}

func TestStartWebUsesTheRecordedAPIPort(t *testing.T) {
	t.Parallel()
	h := newHarness(t, false)
	lease, err := (&FilePortAllocator{LockRoot: portLockRoot(h.env), Owner: h.env.CurrentRoot}).Acquire(freePort(t))
	require.NoError(t, err)
	t.Cleanup(func() { _ = lease.Close() })
	forget, err := recordAPIPort(h.env, lease.Port())
	require.NoError(t, err)
	t.Cleanup(forget)

	require.NoError(t, h.app.Run(t.Context(), []string{"start", "web"}))

	assert.Equal(t, serverURL(lease.Port()), h.processes.servers.APIURL)
	assert.Empty(t, h.database.calls, "the frontend alone doesn't need PostgreSQL")
}

func TestStartWebIgnoresAPortLeftByACrashedLauncher(t *testing.T) {
	t.Parallel()
	h := newHarness(t, false)
	lease, err := (&FilePortAllocator{LockRoot: portLockRoot(h.env), Owner: h.env.CurrentRoot}).Acquire(freePort(t))
	require.NoError(t, err)
	forget, err := recordAPIPort(h.env, lease.Port())
	require.NoError(t, err)
	t.Cleanup(forget)
	// The launcher died without cleaning up: the file remains, the lease doesn't.
	require.NoError(t, lease.Close())

	err = h.app.Run(t.Context(), []string{"start", "web"})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "no API is running in this worktree")
}

func TestStartWebIgnoresAPortAnotherWorktreeLeased(t *testing.T) {
	t.Parallel()
	h := newHarness(t, false)
	lease, err := (&FilePortAllocator{LockRoot: portLockRoot(h.env), Owner: "/some/other/worktree"}).Acquire(freePort(t))
	require.NoError(t, err)
	t.Cleanup(func() { _ = lease.Close() })
	forget, err := recordAPIPort(h.env, lease.Port())
	require.NoError(t, err)
	t.Cleanup(forget)

	err = h.app.Run(t.Context(), []string{"start", "web"})

	require.Error(t, err, "the port this worktree recorded now belongs to another worktree's API")
	assert.Contains(t, err.Error(), "no API is running in this worktree")
}

func TestRecordAPIPortLeavesAnotherAPIsPort(t *testing.T) {
	t.Parallel()
	h := newHarness(t, false)

	forgetFirst, err := recordAPIPort(h.env, 3579)
	require.NoError(t, err)
	forgetSecond, err := recordAPIPort(h.env, 3580)
	require.NoError(t, err)

	forgetFirst()
	contents, err := os.ReadFile(apiPortPath(h.env))
	require.NoError(t, err, "the first API stopping leaves the second's port")
	assert.Equal(t, "3580", string(contents))

	forgetSecond()
	assert.NoFileExists(t, apiPortPath(h.env))
}

func TestStartRejectsUnknownModes(t *testing.T) {
	t.Parallel()
	h := newHarness(t, false)
	assert.Error(t, h.app.Run(t.Context(), []string{"start", "everything"}))
}

func TestConfirmStopsWaitingWhenInterrupted(t *testing.T) {
	t.Parallel()
	h := newHarness(t, false)
	reader, writer, err := os.Pipe()
	require.NoError(t, err)
	t.Cleanup(func() { _ = writer.Close() })
	h.app.Stdin = reader
	ctx, cancel := context.WithCancel(t.Context())
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()

	_, err = h.app.confirm(ctx, "Proceed?")

	assert.ErrorIs(t, err, context.Canceled, "nobody has to press Enter after Ctrl+C")
}

func TestDatabaseStop(t *testing.T) {
	t.Parallel()
	h := newHarness(t, false)

	require.NoError(t, h.app.Run(t.Context(), []string{"db", "stop"}))

	assert.Equal(t, []string{"stop"}, h.database.calls, "stopping doesn't start PostgreSQL first")
}

func TestDatabaseRollback(t *testing.T) {
	t.Parallel()
	h := newHarness(t, false)

	require.NoError(t, h.app.Run(t.Context(), []string{"db", "rollback"}))

	assert.Equal(t, []string{"ensure", "rollback app_worktree"}, h.database.calls)
}

func TestResetAsksBeforeDeletingData(t *testing.T) {
	t.Parallel()
	h := newHarness(t, false)
	h.database.databases["app_worktree"] = true

	require.NoError(t, h.app.Run(t.Context(), []string{"db", "reset"}))
	assert.NotContains(t, h.database.calls, "reset app_worktree")
	assert.Contains(t, h.stdout.String(), "Reset cancelled.")

	h.app.Stdin = strings.NewReader("y\n")
	require.NoError(t, h.app.Run(t.Context(), []string{"db", "reset"}))
	assert.Contains(t, h.database.calls, "reset app_worktree")
	assert.Equal(t, "migrate app_worktree", h.database.calls[len(h.database.calls)-1])
}

func TestTestRunsAgainstTheWorktreeTestDatabase(t *testing.T) {
	t.Parallel()
	h := newHarness(t, false)

	require.NoError(t, h.app.Run(t.Context(), []string{"test"}))

	assert.Equal(t, []string{"ensure", "create app_worktree_test"}, h.database.calls)
	assert.Equal(t, []string{
		"test postgres://postgres:postgres@127.0.0.1:5544/app_worktree_test?sslmode=disable " + strings.Join(defaultTestArgs, " "),
	}, h.processes.calls)
}

func TestTestUsesAnExistingDatabaseWhenTold(t *testing.T) {
	t.Parallel()
	h := newHarness(t, false)
	h.getenv[testDatabaseURLEnv] = "postgres://ci:ci@127.0.0.1:5432/ci_test"

	require.NoError(t, h.app.Run(t.Context(), []string{"test", "-race", "./pkg/config"}))

	assert.Empty(t, h.database.calls, "Docker isn't touched")
	assert.Equal(t, []string{"test postgres://ci:ci@127.0.0.1:5432/ci_test -race ./pkg/config"}, h.processes.calls)
}

func TestCloneRequiresALinkedWorktree(t *testing.T) {
	t.Parallel()
	h := newHarness(t, true)
	assert.Error(t, h.app.Run(t.Context(), []string{"clone"}))
}

func TestCloneAsksBeforeReplacingData(t *testing.T) {
	t.Parallel()
	h := newHarness(t, false)
	h.database.databases["app"] = true
	h.database.databases["app_worktree"] = true

	require.NoError(t, h.app.Run(t.Context(), []string{"clone"}))
	assert.NotContains(t, h.database.calls, "clone app app_worktree")
	assert.Contains(t, h.stdout.String(), "Clone cancelled.")

	h.app.Stdin = strings.NewReader("y\n")
	require.NoError(t, h.app.Run(t.Context(), []string{"clone"}))
	assert.Contains(t, h.database.calls, "clone app app_worktree")
}

func TestCloneToMainSwapsDirection(t *testing.T) {
	t.Parallel()
	h := newHarness(t, false)
	h.database.databases["app_worktree"] = true
	h.database.databases["app"] = true
	h.app.Stdin = strings.NewReader("y\n")

	require.NoError(t, h.app.Run(t.Context(), []string{"clone", "--to-main"}))

	assert.Contains(t, h.database.calls, "clone app_worktree app")
}

func TestPsqlSetsUpAMissingDatabase(t *testing.T) {
	t.Parallel()
	h := newHarness(t, false)

	require.NoError(t, h.app.Run(t.Context(), []string{"psql"}))

	assert.Equal(t, []string{"ensure", "create app_worktree", "migrate app_worktree", "psql app_worktree"}, h.database.calls)
}

func TestPsqlLeavesAnExistingDatabaseAlone(t *testing.T) {
	t.Parallel()
	h := newHarness(t, false)
	h.database.databases["app_worktree"] = true

	require.NoError(t, h.app.Run(t.Context(), []string{"psql"}))

	assert.Equal(t, []string{"ensure", "psql app_worktree"}, h.database.calls, "an existing database isn't migrated, so a rollback stays visible")
}

func TestPsqlTestCreatesTheTestDatabase(t *testing.T) {
	t.Parallel()
	h := newHarness(t, false)

	require.NoError(t, h.app.Run(t.Context(), []string{"psql", "--test"}))

	assert.Equal(t, []string{"ensure", "create app_worktree_test", "psql app_worktree_test"}, h.database.calls)
}

func TestE2ERebuildsItsDatabaseAndRunsOneBrowser(t *testing.T) {
	t.Parallel()
	h := newHarness(t, false)

	require.NoError(t, h.app.Run(t.Context(), []string{"e2e", "chromium", "e2e/app.spec.ts"}))

	assert.Equal(t, []string{"ensure", "reset app_worktree_e2e", "migrate app_worktree_e2e"}, h.database.calls)
	assert.Equal(t, []string{
		"e2e postgres://postgres:postgres@127.0.0.1:5544/app_worktree_e2e?sslmode=disable on 4579 --project=chromium e2e/app.spec.ts",
	}, h.processes.calls)
}

func TestE2ERunsEveryBrowserByDefault(t *testing.T) {
	t.Parallel()
	for _, args := range [][]string{{"e2e"}, {"e2e", "all"}, {"e2e", "--headed"}} {
		h := newHarness(t, false)

		require.NoError(t, h.app.Run(t.Context(), args))

		require.Len(t, h.processes.calls, 1)
		assert.NotContains(t, h.processes.calls[0], "--project", "every browser runs")
	}
}

func TestDatabaseURLPointsAtTheSharedContainer(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "postgres://postgres:postgres@127.0.0.1:5544/app?sslmode=disable", DatabaseURL(5544, "app"))
}

func TestPostgresPortConflictsAreRecognized(t *testing.T) {
	t.Parallel()

	assert.True(t, postgresPortConflict([]byte("Bind for 127.0.0.1:5432 failed: port is already allocated")))
	assert.True(t, postgresPortConflict([]byte("listen tcp 127.0.0.1:5432: bind: address already in use")))
	assert.False(t, postgresPortConflict([]byte("database files are incompatible with server")))
}

func TestParsePublishedPort(t *testing.T) {
	t.Parallel()

	for output, expected := range map[string]int{
		"127.0.0.1:5544\n":          5544,
		"[::]:5545\n0.0.0.0:5545\n": 5545,
		"0.0.0.0:5432":              5432,
	} {
		port, err := parsePublishedPort(output)
		require.NoError(t, err)
		assert.Equal(t, expected, port)
	}
	for _, output := range []string{"", "no port", "127.0.0.1:0", "127.0.0.1:99999"} {
		_, err := parsePublishedPort(output)
		assert.Error(t, err, output)
	}
}

func TestWarnsAboutDataLeftInTmpPostgres(t *testing.T) {
	t.Parallel()

	env := Environment{MainRoot: t.TempDir()}
	var out bytes.Buffer
	database := &DockerDatabase{Stdout: &out}

	database.warnAboutBindMountData(env)
	assert.Empty(t, out.String())

	old := filepath.Join(env.MainRoot, "tmp", "postgres")
	require.NoError(t, os.MkdirAll(old, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(old, "PG_VERSION"), []byte("17\n"), 0o600))
	database.warnAboutBindMountData(env)
	assert.Contains(t, out.String(), "Upgrading from tmp/postgres")
}

func TestComposeCommandsShareTheMainWorktreeProject(t *testing.T) {
	t.Parallel()

	mainRoot := t.TempDir()
	currentRoot := t.TempDir()
	env := Environment{MainRoot: mainRoot, CurrentRoot: currentRoot, ProjectName: "app_1234"}

	command := (&DockerDatabase{}).composeCommand(t.Context(), env, 5544, "ps")
	assert.Equal(t, mainRoot, command.Dir)
	assert.Equal(t, []string{
		"docker", "compose",
		"--project-name", "app_1234",
		"--project-directory", mainRoot,
		"--file", filepath.Join(currentRoot, "compose.yaml"),
		"ps",
	}, command.Args)
	assert.Equal(t, "5544", lastEnvironmentValue(command.Env, "POSTGRES_PORT"))
}

func TestServerEnvironmentConfiguresTheDevServers(t *testing.T) {
	t.Parallel()

	env := Environment{CurrentRoot: "/worktrees/feature"}
	values := serverEnvironment(env, Servers{
		DatabaseURL:     "postgres://postgres:postgres@127.0.0.1:5544/app_feature?sslmode=disable",
		APIPort:         3580,
		WebPort:         5174,
		APIURL:          "http://127.0.0.1:3580",
		CookieNamespace: "app_feature",
	})

	assert.Equal(t, "postgres://postgres:postgres@127.0.0.1:5544/app_feature?sslmode=disable", lastEnvironmentValue(values, "DATABASE_URL"))
	assert.Equal(t, "/worktrees/feature/app.dev.yaml", lastEnvironmentValue(values, "CONFIG_FILE"))
	assert.Equal(t, "3580", lastEnvironmentValue(values, "SERVER_PORT"))
	assert.Equal(t, "5174", lastEnvironmentValue(values, "WEB_PORT"))
	assert.Equal(t, "http://127.0.0.1:3580", lastEnvironmentValue(values, "VITE_API_URL"))
	assert.Equal(t, "app_feature", lastEnvironmentValue(values, "COOKIE_NAMESPACE"))
}

func TestReadPostgresPort(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	port, err := readPostgresPort(root)
	require.NoError(t, err)
	assert.Zero(t, port, "no port is chosen before PostgreSQL first starts")

	require.NoError(t, writePostgresPort(root, 5544))
	port, err = readPostgresPort(root)
	require.NoError(t, err)
	assert.Equal(t, 5544, port)
}

func TestRepositoryIdentityComesFromMainWorktreeDirectory(t *testing.T) {
	t.Parallel()

	mainRoot := filepath.Join("/tmp", "Order Console")
	currentRoot := filepath.Join("/tmp", "feature-worktree")

	assert.Equal(t, "order_console", worktreeDatabaseName(mainRoot, mainRoot))
	assert.Regexp(t, `^order_console_feature_worktree_[a-f0-9]{8}$`, worktreeDatabaseName(mainRoot, currentRoot))
	assert.Regexp(t, `^order_console_[a-f0-9]{8}$`, projectName(mainRoot))
}

func TestWorktreeDatabaseNamesLeaveRoomForSuffixes(t *testing.T) {
	t.Parallel()

	currentRoot := filepath.Join("/tmp", "A feature worktree with a name that is much longer than a PostgreSQL identifier can be")
	env := Environment{CurrentDatabase: worktreeDatabaseName("/tmp/main", currentRoot)}
	assert.Equal(t, env.CurrentDatabase, worktreeDatabaseName("/tmp/main", currentRoot))
	assert.Regexp(t, `^[a-z0-9_]+$`, env.CurrentDatabase)
	assert.LessOrEqual(t, len(env.TestDatabase()), maxIdentifierLength)
	assert.LessOrEqual(t, len(env.E2EDatabase()), maxIdentifierLength)
	assert.LessOrEqual(t, len(env.CookieNamespace()), 64)
}

func lastEnvironmentValue(values []string, key string) string {
	prefix := key + "="
	for _, value := range slices.Backward(values) {
		if value, found := strings.CutPrefix(value, prefix); found {
			return value
		}
	}
	return ""
}
