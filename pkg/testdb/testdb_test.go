package testdb_test

import (
	"testing"

	"github.com/robinjoseph08/web-app-template/pkg/testdb"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewIsolatesEachTest(t *testing.T) {
	t.Parallel()

	first := testdb.New(t)
	second := testdb.New(t)

	_, err := first.ExecContext(t.Context(), "CREATE TABLE widgets (id int)")
	require.NoError(t, err)

	var tables int
	require.NoError(t, second.NewSelect().
		ColumnExpr("count(*)").
		TableExpr("information_schema.tables").
		Where("table_schema = current_schema()").
		Where("table_name = 'widgets'").
		Scan(t.Context(), &tables))
	assert.Zero(t, tables, "a table in one test's schema is invisible to another's")
}

func TestNewAppliesMigrations(t *testing.T) {
	t.Parallel()

	db := testdb.New(t)

	var tables int
	require.NoError(t, db.NewSelect().
		ColumnExpr("count(*)").
		TableExpr("information_schema.tables").
		Where("table_schema = current_schema()").
		Where("table_name = 'bun_migrations'").
		Scan(t.Context(), &tables))
	assert.Equal(t, 1, tables)
}

func TestOpenDoesNotExposeMalformedURLCredentials(t *testing.T) {
	t.Parallel()

	db, err := testdb.Open("postgres://user:secret@localhost:bad/db")
	require.Error(t, err)
	assert.Nil(t, db)
	assert.NotContains(t, err.Error(), "secret")
}
