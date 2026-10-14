package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/creasty/defaults"
)

func TestExpandEnv(t *testing.T) {
	t.Setenv("GINADMIN_TEST_SET", "from-env")
	t.Setenv("GINADMIN_TEST_TRICKY", `p"a\tss\word ${not-a-ref}`)

	type hook struct{ Options map[string]string }
	type conf struct {
		Plain    string
		Set      string
		Default  string
		Empty    string
		Bcrypt   string
		Tricky   string
		List     []string
		Hooks    []*hook
		unexport string
	}
	c := &conf{
		Plain:    "no references",
		Set:      "x-${GINADMIN_TEST_SET}-y",
		Default:  "${GINADMIN_TEST_UNSET:-fallback}",
		Empty:    "${GINADMIN_TEST_UNSET:-}",
		Bcrypt:   "$2a$10$abcdefghijklmnopqrstuv",
		Tricky:   "${GINADMIN_TEST_TRICKY}",
		List:     []string{"${GINADMIN_TEST_SET}"},
		Hooks:    []*hook{{Options: map[string]string{"DSN": "${GINADMIN_TEST_SET}"}}},
		unexport: "${GINADMIN_TEST_SET}",
	}
	if err := ExpandEnv(c); err != nil {
		t.Fatal(err)
	}
	want := map[string][2]string{
		"plain":   {c.Plain, "no references"},
		"set":     {c.Set, "x-from-env-y"},
		"default": {c.Default, "fallback"},
		"empty":   {c.Empty, ""},
		"bcrypt":  {c.Bcrypt, "$2a$10$abcdefghijklmnopqrstuv"},
		// Values are used verbatim: quotes, backslashes and even ${...} inside
		// a value are not interpreted again.
		"tricky": {c.Tricky, `p"a\tss\word ${not-a-ref}`},
		"slice":  {c.List[0], "from-env"},
		"map":    {c.Hooks[0].Options["DSN"], "from-env"},
	}
	for name, w := range want {
		if w[0] != w[1] {
			t.Errorf("%s: got %q, want %q", name, w[0], w[1])
		}
	}
	if c.unexport != "${GINADMIN_TEST_SET}" {
		t.Error("unexported fields must be left alone")
	}

	missing := &conf{Hooks: []*hook{{Options: map[string]string{"DSN": "${GINADMIN_TEST_MISSING}"}}}}
	err := ExpandEnv(missing)
	if err == nil || !strings.Contains(err.Error(), "GINADMIN_TEST_MISSING (Hooks[0].Options[DSN])") {
		t.Fatalf("missing variable should name the variable and field, got: %v", err)
	}
}

// Environment values used to be spliced into the TOML text before parsing, so
// a quote in a password broke the file and ${...} in comments was required.
func TestLoadExpandsAfterParsing(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "server.toml")
	content := "# A comment mentioning ${GINADMIN_TEST_UNSET_IN_COMMENT} is ignored.\n" +
		"[Storage.Cache.Redis]\nPassword = \"${GINADMIN_TEST_REDIS_PW}\"\n"
	if err := os.WriteFile(file, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GINADMIN_TEST_REDIS_PW", `a"b\nc`)

	*C = Config{}
	t.Cleanup(func() { *C = Config{} })
	if err := Load(dir, "server.toml"); err != nil {
		t.Fatal(err)
	}
	if got := C.Storage.Cache.Redis.Password; got != `a"b\nc` {
		t.Fatalf("password: got %q", got)
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
		{"cert without key", func(c *Config) { c.General.HTTP.CertFile = "cert.pem" }, "KeyFile"},
		{"known root password", func(c *Config) { c.General.Root.Password = knownDefaultPassword }, "Root.Password"},
		{"known default password", func(c *Config) { c.General.DefaultLoginPwd = knownDefaultPassword }, "DefaultLoginPwd"},
		{"auth disabled", func(c *Config) { c.Middleware.Auth.Disable = true }, "Auth.Disable"},
		{"plain md5 root password", func(c *Config) { c.General.Root.Password = "e10adc3949ba59abbe56e057f20f883e" }, "bcrypt"},
		{"casbin disabled", func(c *Config) { c.Middleware.Casbin.Disable = true }, "Casbin.Disable"},
		{"pprof on all interfaces", func(c *Config) { c.General.PprofAddr = ":6060" }, "PprofAddr"},
		{"pprof on a public address", func(c *Config) { c.General.PprofAddr = "10.0.0.5:6060" }, "PprofAddr"},
		{"rate limit of zero per IP", func(c *Config) {
			c.Middleware.RateLimiter.Enable, c.Middleware.RateLimiter.Store.Type = true, "memory"
			c.Middleware.RateLimiter.Period, c.Middleware.RateLimiter.MaxRequestsPerUser = 10, 100
		}, "MaxRequestsPerIP"},
		{"metrics without port", func(c *Config) { c.General.MetricsAddr = "127.0.0.1" }, "MetricsAddr"},
		{"metrics on the API address", func(c *Config) { c.General.MetricsAddr = c.General.HTTP.Addr }, "MetricsAddr"},
		{"short old signing key", func(c *Config) { c.Middleware.Auth.OldSigningKey = "short" }, "OldSigningKey"},
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

func TestValidateAcceptsSafeProductionValues(t *testing.T) {
	c := newDefaultConfig(t)
	c.General.Root.Password = "$2a$10$gati1dUFQOWDoptaMRE.au7Em0ItckL7a9L0Vqnw.XARKGd9zt.WC"
	c.General.PprofAddr = "127.0.0.1:6060"
	c.Middleware.Auth.OldSigningKey = strings.Repeat("k", 32)
	if err := c.Validate(); err != nil {
		t.Fatalf("expected a valid config, got: %v", err)
	}
	for _, addr := range []string{"localhost:6060", "[::1]:6060"} {
		c.General.PprofAddr = addr
		if err := c.Validate(); err != nil {
			t.Errorf("PprofAddr %q should be accepted: %v", addr, err)
		}
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
