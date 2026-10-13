package config

import (
	"strings"
	"testing"

	"github.com/creasty/defaults"
)

func TestExpandEnv(t *testing.T) {
	t.Setenv("GINADMIN_TEST_SET", "from-env")

	tests := []struct {
		name    string
		in      string
		want    string
		wantErr bool
	}{
		{name: "set variable", in: `Key = "${GINADMIN_TEST_SET}"`, want: `Key = "from-env"`},
		{name: "set variable ignores default", in: `Key = "${GINADMIN_TEST_SET:-fallback}"`, want: `Key = "from-env"`},
		{name: "unset variable uses default", in: `Key = "${GINADMIN_TEST_UNSET:-fallback}"`, want: `Key = "fallback"`},
		{name: "unset variable empty default", in: `Key = "${GINADMIN_TEST_UNSET:-}"`, want: `Key = ""`},
		{name: "unset variable without default", in: `Key = "${GINADMIN_TEST_UNSET}"`, wantErr: true},
		{name: "bcrypt hash untouched", in: `Password = "$2a$10$abcdefghijklmnopqrstuv"`, want: `Password = "$2a$10$abcdefghijklmnopqrstuv"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := expandEnv([]byte(tt.in))
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected an error, got %q", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if string(got) != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func newDefaultConfig(t *testing.T) *Config {
	t.Helper()
	c := new(Config)
	if err := defaults.Set(c); err != nil {
		t.Fatal(err)
	}
	return c
}

func TestValidateDefaults(t *testing.T) {
	if err := newDefaultConfig(t).Validate(); err != nil {
		t.Fatalf("default config should be valid, got: %v", err)
	}
}

func TestValidateRejects(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(c *Config)
		want   string
	}{
		{"unknown db type", func(c *Config) { c.Storage.DB.Type = "oracle" }, "Storage.DB.Type"},
		{"redis cache without addr", func(c *Config) { c.Storage.Cache.Type = "redis" }, "Storage.Cache.Redis.Addr"},
		{"casbin interval zero", func(c *Config) { c.Middleware.Casbin.AutoLoadInterval = 0 }, "AutoLoadInterval"},
		{"casbin no threads", func(c *Config) { c.Middleware.Casbin.LoadThread = 0 }, "LoadThread"},
		{"cert without key", func(c *Config) { c.General.HTTP.CertFile = "cert.pem" }, "KeyFile"},
		{"known root password", func(c *Config) { c.General.Root.Password = knownDefaultPassword }, "Root.Password"},
		{"known default password", func(c *Config) { c.General.DefaultLoginPwd = knownDefaultPassword }, "DefaultLoginPwd"},
		{"auth disabled", func(c *Config) { c.Middleware.Auth.Disable = true }, "Auth.Disable"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := newDefaultConfig(t)
			tt.mutate(c)
			err := c.Validate()
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("expected an error mentioning %q, got: %v", tt.want, err)
			}
		})
	}
}

func TestValidateAllowsKnownPasswordsInDebug(t *testing.T) {
	c := newDefaultConfig(t)
	c.General.Debug = true
	c.General.Root.Password = knownDefaultPassword
	c.General.DefaultLoginPwd = knownDefaultPassword
	if err := c.Validate(); err != nil {
		t.Fatalf("debug config should be valid, got: %v", err)
	}
}

func TestStringRedactsSecrets(t *testing.T) {
	c := newDefaultConfig(t)
	secrets := []string{"root-secret", "default-secret", "redis-secret", "signing-secret", "user:pass@tcp(db)/app", "replica-dsn"}
	c.General.Root.Password = secrets[0]
	c.General.DefaultLoginPwd = secrets[1]
	c.Storage.Cache.Redis.Password = secrets[2]
	c.Middleware.Auth.SigningKey = secrets[3]
	c.Storage.DB.DSN = secrets[4]
	c.Storage.DB.Resolver = append(c.Storage.DB.Resolver, struct {
		DBType   string
		Sources  []string
		Replicas []string
		Tables   []string
	}{DBType: "mysql", Replicas: []string{secrets[5]}})

	out := c.String()
	for _, s := range secrets {
		if strings.Contains(out, s) {
			t.Errorf("printed config leaks %q", s)
		}
	}
	if !strings.Contains(out, redactedValue) {
		t.Error("printed config has no redacted values")
	}
	if !strings.Contains(out, `"Username": "admin"`) {
		t.Error("non-secret values should be printed unchanged")
	}
}
