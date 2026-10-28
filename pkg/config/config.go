package config

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/go-playground/validator/v10"
	"github.com/knadh/koanf/parsers/yaml"
	"github.com/knadh/koanf/providers/env"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/v2"
	"github.com/robinjoseph08/web-app-template/pkg/errorstack"
)

const defaultConfigPath = "/config/app.yaml"

// The values AppEnv accepts.
const (
	EnvironmentProduction  = "production"
	EnvironmentDevelopment = "development"
	EnvironmentTest        = "test"
)

// Config holds the application configuration. Values can come from a YAML
// file or environment variables. Environment variables take precedence.
type Config struct {
	AppEnv                    string        `koanf:"app_env" json:"app_env" validate:"oneof=production development test"`
	CookieNamespace           string        `koanf:"cookie_namespace" json:"-"`
	DatabaseURL               string        `koanf:"database_url" json:"database_url" validate:"required"`
	DatabaseMaxOpenConns      int           `koanf:"database_max_open_conns" json:"database_max_open_conns" validate:"min=1"`
	DatabaseMaxIdleConns      int           `koanf:"database_max_idle_conns" json:"database_max_idle_conns" validate:"min=0,ltefield=DatabaseMaxOpenConns"`
	DatabaseDebug             bool          `koanf:"database_debug" json:"database_debug"`
	DatabaseConnectRetryCount int           `koanf:"database_connect_retry_count" json:"database_connect_retry_count" validate:"min=1"`
	DatabaseConnectRetryDelay time.Duration `koanf:"database_connect_retry_delay" json:"database_connect_retry_delay" validate:"min=0"`
	ServerHost                string        `koanf:"server_host" json:"server_host" validate:"required"`
	ServerPort                int           `koanf:"server_port" json:"server_port" validate:"min=0,max=65535"`
	Hostname                  string        `koanf:"-" json:"-"`
}

func defaults() *Config {
	//nolint:gosec // The default is the local development database.
	return &Config{
		AppEnv:                    EnvironmentProduction,
		CookieNamespace:           "web_app_template",
		DatabaseURL:               "postgres://postgres:postgres@localhost:5432/web_app_template?sslmode=disable",
		DatabaseMaxOpenConns:      10,
		DatabaseMaxIdleConns:      3,
		DatabaseDebug:             false,
		DatabaseConnectRetryCount: 5,
		DatabaseConnectRetryDelay: 2 * time.Second,
		ServerHost:                "0.0.0.0",
		ServerPort:                3579,
	}
}

// CookieName prefixes name with the cookie namespace, so every worktree's dev
// server keeps its own cookies even when two of them end up on the same
// localhost port at different times.
func (c *Config) CookieName(name string) string {
	return c.CookieNamespace + "_" + name
}

// New loads the configuration from defaults, a YAML file, and environment
// variables, in that order.
func New() (*Config, error) {
	configPath := os.Getenv("CONFIG_FILE")
	requireConfigFile := configPath != ""
	if configPath == "" {
		configPath = defaultConfigPath
	}

	return load(configPath, requireConfigFile, env.Provider("", ".", strings.ToLower), os.Hostname)
}

func load(configPath string, requireConfigFile bool, environment koanf.Provider, hostname func() (string, error)) (*Config, error) {
	k := koanf.New(".")
	cfg := defaults()

	if err := k.Load(file.Provider(configPath), yaml.Parser()); err != nil {
		if !os.IsNotExist(err) || requireConfigFile {
			if pathErr, ok := errors.AsType[*os.PathError](err); ok {
				return nil, fmt.Errorf("config file %s: %w", configPath, pathErr)
			}
			// The parser's message can quote the file, secrets included.
			return nil, fmt.Errorf("config file %s: invalid YAML; check syntax and value types", configPath)
		}
	}

	// The file only holds settings, so anything else is a typo or a setting
	// that no longer exists. Environment variables aren't checked, since they
	// include every unrelated variable in the process.
	known := knownSettings()
	for _, key := range k.Keys() {
		if !known[key] {
			return nil, fmt.Errorf("%s: unknown setting", key)
		}
	}

	if environment != nil {
		if err := k.Load(environment, nil); err != nil {
			return nil, fmt.Errorf("load environment variables: %w", errorstack.Capture(err))
		}
	}

	// Decode one setting at a time so errors identify the field without
	// exposing values from the decoder's error message.
	fields := reflect.ValueOf(cfg).Elem()
	for i := range fields.NumField() {
		name := fields.Type().Field(i).Tag.Get("koanf")
		if name == "-" || !k.Exists(name) {
			continue
		}
		if fields.Field(i).Kind() == reflect.String {
			if _, ok := k.Get(name).(string); !ok {
				return nil, fmt.Errorf("%s: must be a string", name)
			}
		}
		if err := k.Unmarshal(name, fields.Field(i).Addr().Interface()); err != nil {
			return nil, fmt.Errorf("%s: invalid value", name)
		}
	}

	name, err := hostname()
	if err != nil {
		return nil, fmt.Errorf("get hostname: %w", errorstack.Capture(err))
	}
	cfg.Hostname = name

	if err := validateConfig(cfg); err != nil {
		return nil, err
	}

	return cfg, nil
}

func knownSettings() map[string]bool {
	known := make(map[string]bool)
	for field := range reflect.TypeFor[Config]().Fields() {
		if name := field.Tag.Get("koanf"); name != "-" {
			known[name] = true
		}
	}
	return known
}

// NewForTest returns configuration suitable for unit tests that do not connect
// to a database.
func NewForTest() *Config {
	cfg := defaults()
	cfg.AppEnv = EnvironmentTest
	cfg.DatabaseURL = "postgres://test:test@localhost:5432/web_app_template_test?sslmode=disable"
	cfg.DatabaseMaxOpenConns = 3
	cfg.DatabaseMaxIdleConns = 1
	cfg.DatabaseConnectRetryCount = 1
	cfg.DatabaseConnectRetryDelay = 0
	cfg.DatabaseDebug = true
	cfg.ServerHost = "127.0.0.1"
	cfg.ServerPort = 0
	cfg.Hostname = "test-host"
	return cfg
}

func validateConfig(cfg *Config) error {
	validate := validator.New()
	validate.RegisterTagNameFunc(func(field reflect.StructField) string { return field.Tag.Get("koanf") })
	if err := validate.Struct(cfg); err != nil {
		validationErrors, ok := errors.AsType[validator.ValidationErrors](err)
		if !ok {
			return errorstack.Capture(err)
		}
		field := validationErrors[0]
		return fmt.Errorf("%s: failed %s", field.Field(), strings.TrimSpace(field.Tag()+" "+field.Param()))
	}
	if err := validateDatabaseURL(cfg.DatabaseURL); err != nil {
		return err
	}
	if !validCookieNamespace(cfg.CookieNamespace) {
		return errors.New("cookie_namespace: must be 64 characters or fewer and use lowercase letters, numbers, and underscores")
	}
	return nil
}

func validateDatabaseURL(value string) error {
	u, err := parseURL("database_url", value)
	if err != nil {
		return err
	}
	if u.Scheme != "postgres" && u.Scheme != "postgresql" {
		return errors.New("database_url: must use postgres or postgresql")
	}
	if u.User == nil || strings.TrimSpace(u.User.Username()) == "" {
		return errors.New("database_url: must include a database role")
	}
	name := strings.TrimPrefix(u.Path, "/")
	if strings.TrimSpace(name) == "" || strings.Contains(name, "/") {
		return errors.New("database_url: must include one database name")
	}
	if _, err := url.ParseQuery(u.RawQuery); err != nil {
		return errors.New("database_url: invalid query parameters")
	}
	return nil
}

// parseURL reports the field, never the input, because URLs can contain
// secrets.
func parseURL(field, value string) (*url.URL, error) {
	u, err := url.Parse(value)
	if err != nil {
		return nil, fmt.Errorf("%s: invalid URL", field)
	}
	if u.Hostname() == "" || u.Opaque != "" {
		return nil, fmt.Errorf("%s: must include a host", field)
	}
	if (strings.Contains(u.Hostname(), ":") || strings.HasPrefix(u.Host, "[")) && net.ParseIP(u.Hostname()) == nil {
		return nil, fmt.Errorf("%s: invalid host", field)
	}
	if strings.HasSuffix(u.Host, ":") {
		return nil, fmt.Errorf("%s: invalid port", field)
	}
	if port := u.Port(); port != "" {
		number, err := strconv.Atoi(port)
		if err != nil || number < 1 || number > 65535 {
			return nil, fmt.Errorf("%s: invalid port", field)
		}
	}
	if u.Fragment != "" || strings.Contains(value, "#") {
		return nil, fmt.Errorf("%s: must not contain a fragment", field)
	}
	return u, nil
}

func validCookieNamespace(value string) bool {
	if value == "" || len(value) > 64 {
		return false
	}
	for _, character := range value {
		if (character >= 'a' && character <= 'z') || (character >= '0' && character <= '9') || character == '_' {
			continue
		}
		return false
	}
	return true
}
