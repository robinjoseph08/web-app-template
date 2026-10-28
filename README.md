# Web App Template

A full-stack web application template with React, Go, PostgreSQL, and one production binary.

<!-- web-app-template:template-only:start -->

## Create an application

Mark this repository as a template in GitHub, then choose **Use this template** to create a repository with independent history. Clone the generated repository and initialize its identity:

```sh
mise init github.com/owner/repository
```

The command derives the package slug, database identifier, and display name from the final segment of the module path. For example, `github.com/owner/order-console` becomes `order-console`, `order_console`, and `Order Console`. Review and commit the resulting changes before starting application work.

GitHub copies workflow files but does not copy repository secrets, rulesets, branch protection, environments, or other repository settings. Configure those separately in the generated repository.

<!-- web-app-template:template-only:end -->

## Prerequisites

Install [mise](https://mise.jdx.dev/) and Docker Desktop or another Docker engine with Compose support. The development commands use Unix process groups and file locks, so native Windows development requires WSL.

## Set up a worktree

Run setup in any worktree, main or linked:

```sh
mise setup
```

This command installs the pinned tools, JavaScript dependencies, and the Chromium, Firefox, and WebKit browsers. It starts PostgreSQL with Docker Compose if it isn't running, creates this worktree's database and Go test database, runs migrations, and generates TypeScript types.

PostgreSQL is shared by every worktree, but each worktree receives separate databases. Any command that needs PostgreSQL starts it, and a lock keeps two worktrees from starting it at once. Its data lives in a Docker volume rather than a directory, because Docker Desktop's file sharing doesn't give PostgreSQL reliable fsync. Its host port is recorded in the main worktree's `tmp/database.env`. Setup prefers port `5432` and uses the next available port when necessary. A running container's published port always wins over the recorded one.

A linked worktree's first database is a copy of the main worktree's when that exists, and empty otherwise. Later runs preserve an existing worktree database. Worktree database names use the repository name, worktree directory, and a path hash.

Stop the shared container with `mise db:stop`. Its data is kept. Do not run `docker compose down -v`, which deletes every worktree's data.

## Start development

```sh
mise start
```

The command starts the API with Air, waits for it to become ready, and then starts Vite. It tries API port `3579` and web port `5173` first. If either port is occupied on any local address, or leased by another worktree, it uses the next available port. Vite proxies `/api` and `/health` to the selected API port.

The following commands start one process when needed:

```sh
mise start:air
mise start:api
mise start:web
```

`start:web` proxies to the API already running in the same worktree, so start one first with `start:air` or `start:api` in another terminal. It refuses to guess a port, which could reach another worktree's API and database.

Every worktree's dev server can end up on the same localhost port, and browsers keep cookies per host. The launcher sets `COOKIE_NAMESPACE` to the worktree's database name, so name cookies with `cfg.CookieName` to keep each worktree's sessions apart.

The API logs JSON to stdout unless `LOG_FORMAT` says otherwise. The launcher sets `LOG_FORMAT=console` for readable development logs.

Open `psql` on the worktree's database, or its Go test database, with:

```sh
mise psql
mise psql --test
```

## Run checks

```sh
mise check
```

This runs Go linting, Go tests, ESLint, Prettier, TypeScript checks, Vitest, Chromium E2E tests, and a complete production build. CI also runs the race detector, Firefox, WebKit, a Docker build, and a production smoke test.

Other useful commands:

```sh
mise check:quiet
mise test:race
mise test:e2e
mise e2e:firefox
mise e2e:webkit
```

### Go tests

`mise test:go` runs the Go tests against the worktree's test database. Tests that need PostgreSQL call `testdb.New(t)`, which creates a schema for that test, applies every migration, and drops the schema afterward, so tests can run in parallel without seeing each other's rows. Share one schema between a test's subtests so migrations don't run once per case. The tasks pass `-parallel 4` so many parallel tests can't exhaust PostgreSQL's connections.

Set `TEST_DATABASE_URL` to run the tests against another database, as CI does. Tests run outside the mise tasks, like from an editor, use the worktree's test database that `mise setup` creates.

### E2E tests

The E2E tasks build the production binary and run Playwright against it, the way the application ships. The server uses its own database, rebuilt and migrated on every run, and writes its log to `build/e2e/server.log`. Extra arguments go to Playwright:

```sh
mise e2e:chromium e2e/app.spec.ts --headed
```

Tests import `test` and `expect` from `e2e/fixtures.ts`, which blocks requests to any other origin and fails a test on uncaught page errors.

## Build the production application

```sh
mise build
```

Vite writes its production output into the Go web package. Go embeds those files into `build/api/api`. The binary serves API routes, static assets, and SPA fallback routes from one HTTP port. Development still uses Vite separately for hot module replacement.

Build the production container:

```sh
mise docker
```

The image runs one non-root Go process, listens on port `3579`, and reads optional configuration from `/config/app.yaml`. Supply `DATABASE_URL` for a reachable PostgreSQL database. See `app.example.yaml` for every setting. The file may only contain those settings, so a typo fails at startup instead of being ignored. Configuration errors name the setting without echoing its value, since values like `database_url` can hold passwords.

The binary compresses the frontend's files once at startup and serves them with an ETag per encoding. Hashed assets are cached forever, and the HTML can't be framed by other sites. API responses are compressed per request and never cached. API requests that change state must come from the same origin and send JSON bodies of up to 64 KB.

## Database migrations

```sh
mise db:migrate
mise db:rollback
mise db:reset
mise db:migrate:create migration_name
```

`db:reset` deletes all data in the current worktree database and asks for confirmation when the database exists.

## Clone worktree data

Clone the main worktree's database into the current linked worktree, or reverse the direction to replace the main worktree's database:

```sh
mise clone
mise clone --to-main
```

Clone commands must run from a linked worktree. Replacing an existing database requires confirmation. The copy restores into a staging database first, so the destination is only replaced once the copy is complete.

## Frontend

Import from `app/` with the `@/` alias, like `import { App } from "@/App"`.

The theme follows the device's color scheme until the visitor picks one with the toggle. An inline script in `app/index.html` applies it before the page draws, so it doesn't flash light first. Colors are named for their role in `app/styles.css` (`bg-surface`, `text-ink`, `border-line`, and so on) and defined once per theme, so components rarely need `dark:` classes.

Go types listed in `tygo.yaml` are generated into `app/types/generated`. `GET /api/version` is the example: the frontend shows the server's version from it.

## Releases

Create a release after the default branch is clean and pushed:

```sh
mise release 1.2.3 --dry-run
mise release 1.2.3
```

The release command updates `CHANGELOG.md` and `package.json`, creates a `[Release] v1.2.3` commit and annotated tag, then atomically pushes both. The tag-triggered workflow validates the repository, creates a GitHub release, and publishes `linux/amd64` and `linux/arm64` images to GitHub Container Registry.

<!-- web-app-template:template-only:start -->

## Template maintenance

Keep the template buildable with its neutral identity. Before marking a revision ready for reuse, verify:

```sh
mise setup
mise check
mise test:race
mise test:e2e
mise docker
```

Generate a temporary repository and run `mise init` there whenever the initializer or project identity changes.

<!-- web-app-template:template-only:end -->
