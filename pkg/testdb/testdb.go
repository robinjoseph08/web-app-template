// Package testdb gives each database test its own migrated PostgreSQL schema,
// so tests can run in parallel without seeing each other's rows.
package testdb

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/robinjoseph08/web-app-template/internal/devtool"
	"github.com/robinjoseph08/web-app-template/pkg/database"
	"github.com/robinjoseph08/web-app-template/pkg/errorstack"
	"github.com/robinjoseph08/web-app-template/pkg/migrations"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/pgdialect"
)

// New returns a connection to a new schema with every migration applied. The
// schema is dropped when the test ends. Call it once per top-level test and
// share the connection between that test's subtests, so migrations don't run
// once per case.
//
// The schema is created in the database TEST_DATABASE_URL names, which mise
// test:go sets and CI must set. Otherwise it's this worktree's test database,
// which mise setup creates.
func New(tb testing.TB) *bun.DB {
	tb.Helper()
	ctx := tb.Context()
	started := time.Now()

	baseURL := os.Getenv("TEST_DATABASE_URL")
	if baseURL == "" {
		if os.Getenv("CI") == "true" && os.Getenv("GITHUB_ACTIONS") == "true" {
			tb.Fatal("TEST_DATABASE_URL is required in CI")
		}
		var err error
		if baseURL, err = devtool.TestDatabaseURL(ctx); err != nil {
			tb.Fatalf("find the test database: %v", err)
		}
	}
	schema, err := NewSchema(ctx, baseURL)
	if err != nil {
		tb.Fatalf("test database: %v; run mise setup or set TEST_DATABASE_URL", err)
	}
	tb.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := schema.Close(ctx); err != nil {
			tb.Errorf("drop test schema: %v", err)
		}
	})
	db, err := Open(schema.URL)
	if err != nil {
		tb.Fatal(err)
	}
	tb.Cleanup(func() {
		if err := db.Close(); err != nil {
			tb.Error(err)
		}
	})
	if _, err := migrations.BringUpToDate(ctx, db); err != nil {
		tb.Fatal(err)
	}
	tb.Logf("isolated database setup including migrations: %s", time.Since(started))
	return db
}

// Open connects with a small pool, so parallel test packages don't exhaust
// PostgreSQL's connections.
func Open(dsn string) (*bun.DB, error) {
	sqlDB, err := database.OpenSQL(dsn)
	if err != nil {
		return nil, err
	}
	sqlDB.SetMaxOpenConns(3)
	sqlDB.SetMaxIdleConns(1)
	return bun.NewDB(sqlDB, pgdialect.New()), nil
}

// Schema is a temporary schema. Close the connections that use it before
// calling Close.
type Schema struct {
	// URL connects to the schema through search_path.
	URL  string
	name string
	db   *bun.DB
}

// NewSchema creates an empty schema without touching anything else in the
// database at baseURL.
func NewSchema(ctx context.Context, baseURL string) (*Schema, error) {
	u, err := url.Parse(baseURL)
	if err != nil {
		// The URL may contain a password, so it isn't quoted.
		return nil, fmt.Errorf("parse test database URL: invalid PostgreSQL URL")
	}
	var random [8]byte
	if _, err := rand.Read(random[:]); err != nil {
		return nil, errorstack.Capture(err)
	}
	name := "test_" + hex.EncodeToString(random[:])

	db, err := Open(baseURL)
	if err != nil {
		return nil, err
	}
	if _, err := db.ExecContext(ctx, "CREATE SCHEMA ?", bun.Ident(name)); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("create test schema: %w", errorstack.CaptureContext(ctx, err))
	}
	query := u.Query()
	query.Set("search_path", name)
	u.RawQuery = query.Encode()
	return &Schema{URL: u.String(), name: name, db: db}, nil
}

// Close drops the schema and everything in it.
func (s *Schema) Close(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, "DROP SCHEMA ? CASCADE", bun.Ident(s.name))
	closeErr := s.db.Close()
	if err != nil {
		return errorstack.CaptureContext(ctx, err)
	}
	return errorstack.Capture(closeErr)
}
