package config

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mapProvider map[string]any

func (p mapProvider) ReadBytes() ([]byte, error) {
	return nil, errors.New("not supported")
}

func (p mapProvider) Read() (map[string]any, error) {
	return p, nil
}

func TestLoadDefaults(t *testing.T) {
	t.Parallel()

	cfg, err := load(filepath.Join(t.TempDir(), "missing.yaml"), false, mapProvider{}, func() (string, error) {
		return "example-host", nil
	})
	require.NoError(t, err)
	assert.Equal(t, "postgres://postgres:postgres@localhost:5432/web_app_template?sslmode=disable", cfg.DatabaseURL)
	assert.Equal(t, 5, cfg.DatabaseConnectRetryCount)
	assert.Equal(t, 2*time.Second, cfg.DatabaseConnectRetryDelay)
	assert.Equal(t, EnvironmentProduction, cfg.AppEnv)
	assert.Equal(t, "web_app_template", cfg.CookieNamespace)
	assert.Equal(t, 10, cfg.DatabaseMaxOpenConns)
	assert.Equal(t, 3, cfg.DatabaseMaxIdleConns)
	assert.Equal(t, "0.0.0.0", cfg.ServerHost)
	assert.Equal(t, 3579, cfg.ServerPort)
	assert.Equal(t, "example-host", cfg.Hostname)
}

func TestLoadYAMLFile(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "app.yaml")
	require.NoError(t, os.WriteFile(path, []byte(`
database_url: postgres://yaml:secret@db:5432/yaml_db?sslmode=disable
database_debug: true
database_connect_retry_count: 2
database_connect_retry_delay: 25ms
app_env: development
database_max_open_conns: 4
database_max_idle_conns: 2
server_host: 127.0.0.1
server_port: 4000
`), 0o600))

	cfg, err := load(path, false, mapProvider{}, func() (string, error) { return "host", nil })
	require.NoError(t, err)
	assert.Equal(t, "postgres://yaml:secret@db:5432/yaml_db?sslmode=disable", cfg.DatabaseURL)
	assert.True(t, cfg.DatabaseDebug)
	assert.Equal(t, 2, cfg.DatabaseConnectRetryCount)
	assert.Equal(t, 25*time.Millisecond, cfg.DatabaseConnectRetryDelay)
	assert.Equal(t, EnvironmentDevelopment, cfg.AppEnv)
	assert.Equal(t, 4, cfg.DatabaseMaxOpenConns)
	assert.Equal(t, 2, cfg.DatabaseMaxIdleConns)
	assert.Equal(t, "127.0.0.1", cfg.ServerHost)
	assert.Equal(t, 4000, cfg.ServerPort)
}

func TestEnvironmentOverridesFile(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "app.yaml")
	require.NoError(t, os.WriteFile(path, []byte("server_port: 4000\n"), 0o600))

	cfg, err := load(path, false, mapProvider{
		"database_url": "postgres://env:secret@db:5432/env_db?sslmode=disable",
		"server_port":  "5000",
	}, func() (string, error) { return "host", nil })
	require.NoError(t, err)
	assert.Equal(t, "postgres://env:secret@db:5432/env_db?sslmode=disable", cfg.DatabaseURL)
	assert.Equal(t, 5000, cfg.ServerPort)
}

func TestLoadValidatesConfig(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "app.yaml")
	require.NoError(t, os.WriteFile(path, []byte("database_url: \"\"\n"), 0o600))

	_, err := load(path, false, mapProvider{}, func() (string, error) { return "host", nil })
	require.Error(t, err)
	assert.EqualError(t, err, "database_url: failed required")
}

func TestLoadRejectsUnknownFileSettings(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "app.yaml")
	require.NoError(t, os.WriteFile(path, []byte("files_path: /data/files\n"), 0o600))

	_, err := load(path, false, mapProvider{}, func() (string, error) { return "host", nil })
	require.EqualError(t, err, "files_path: unknown setting")
}

func TestLoadIgnoresUnrelatedEnvironmentVariables(t *testing.T) {
	t.Parallel()

	cfg, err := load(filepath.Join(t.TempDir(), "missing.yaml"), false, mapProvider{"path": "/usr/bin"}, func() (string, error) { return "host", nil })
	require.NoError(t, err)
	assert.Equal(t, 3579, cfg.ServerPort)
}

func TestLoadNamesInvalidFieldsWithoutTheirValues(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		settings mapProvider
		message  string
	}{
		{"wrong type", mapProvider{"server_port": "secret-port"}, "server_port: invalid value"},
		{"malformed database URL", mapProvider{"database_url": "postgres://user:secret@db:bad/app"}, "database_url: invalid URL"},
		{"database URL without a role", mapProvider{"database_url": "postgres://db:5432/app"}, "database_url: must include a database role"},
		{"database URL without a database", mapProvider{"database_url": "postgres://user:secret@db:5432/"}, "database_url: must include one database name"},
		{"database URL with another scheme", mapProvider{"database_url": "mysql://user:secret@db:3306/app"}, "database_url: must use postgres or postgresql"},
		{"unknown environment", mapProvider{"app_env": "staging"}, "app_env: failed oneof production development test"},
		{"idle above open connections", mapProvider{"database_max_open_conns": "2", "database_max_idle_conns": "5"}, "database_max_idle_conns: failed ltefield DatabaseMaxOpenConns"},
		{"invalid cookie namespace", mapProvider{"cookie_namespace": "Has-Dashes"}, "cookie_namespace: must be 64 characters or fewer and use lowercase letters, numbers, and underscores"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			_, err := load(filepath.Join(t.TempDir(), "missing.yaml"), false, test.settings, func() (string, error) { return "host", nil })
			require.EqualError(t, err, test.message)
			assert.NotContains(t, err.Error(), "secret")
		})
	}
}

func TestLoadDoesNotQuoteInvalidYAML(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "app.yaml")
	require.NoError(t, os.WriteFile(path, []byte("database_url: [postgres://user:secret@db/app\n"), 0o600))

	_, err := load(path, false, mapProvider{}, func() (string, error) { return "host", nil })
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid YAML")
	assert.NotContains(t, err.Error(), "secret")
}

func TestCookieName(t *testing.T) {
	t.Parallel()

	cfg := NewForTest()
	cfg.CookieNamespace = "app_feature_1234abcd"
	assert.Equal(t, "app_feature_1234abcd_session", cfg.CookieName("session"))
}

func TestLoadRequiresExplicitConfigFile(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "missing.yaml")
	_, err := load(path, true, mapProvider{}, func() (string, error) { return "host", nil })
	require.Error(t, err)
	assert.ErrorIs(t, err, os.ErrNotExist)
}

func TestLoadReturnsHostnameError(t *testing.T) {
	t.Parallel()

	expected := errors.New("hostname unavailable")
	_, err := load(filepath.Join(t.TempDir(), "missing.yaml"), false, mapProvider{}, func() (string, error) {
		return "", expected
	})
	require.ErrorIs(t, err, expected)
}
