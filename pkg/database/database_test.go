package database

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/robinjoseph08/web-app-template/pkg/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOpenUsesPostgreSQL(t *testing.T) {
	t.Parallel()

	db, err := open(config.NewForTest())
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	assert.Contains(t, fmt.Sprintf("%T", db.Dialect()), "pgdialect")
	assert.Equal(t, 3, db.Stats().MaxOpenConnections)
}

func TestOpenRejectsMalformedDatabaseURL(t *testing.T) {
	t.Parallel()

	cfg := config.NewForTest()
	cfg.DatabaseURL = "postgres://user:secret@localhost:bad/app"
	db, err := open(cfg)
	require.Error(t, err)
	assert.Nil(t, db)
	assert.Contains(t, err.Error(), "parse database URL")
	assert.NotContains(t, err.Error(), "secret")
}

func TestNewStopsRetryingWhenCancelled(t *testing.T) {
	t.Parallel()

	cfg := config.NewForTest()
	// Nothing listens on port 1, so every attempt fails.
	cfg.DatabaseURL = "postgres://test:test@127.0.0.1:1/app?sslmode=disable"
	cfg.DatabaseConnectRetryCount = 100
	cfg.DatabaseConnectRetryDelay = time.Hour
	ctx, cancel := context.WithTimeout(t.Context(), 200*time.Millisecond)
	t.Cleanup(cancel)

	started := time.Now()
	db, err := New(ctx, cfg)
	require.Error(t, err)
	assert.Nil(t, db)
	assert.Less(t, time.Since(started), 10*time.Second)
}

func TestWithLogging(t *testing.T) {
	t.Parallel()

	ctx := WithLogging(context.Background())
	enabled, ok := ctx.Value(loggingContextKey).(bool)
	require.True(t, ok)
	assert.True(t, enabled)
}
