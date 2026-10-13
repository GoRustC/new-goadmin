package config

import (
	"os"
	"strings"
	"testing"
)

// The production sample under configs/prod must load and validate once its
// required environment variables are set, so it cannot silently rot.
func loadProdSample(t *testing.T) error {
	t.Helper()
	*C = Config{}
	t.Cleanup(func() { *C = Config{} })
	return Load("../../configs", "prod")
}

func setProdEnv(t *testing.T) {
	t.Helper()
	t.Setenv("GINADMIN_DB_DSN", "user:pass@tcp(db:3306)/ginadmin?parseTime=True")
	t.Setenv("GINADMIN_REDIS_ADDR", "redis:6379")
	t.Setenv("GINADMIN_JWT_KEY", strings.Repeat("k", 64))
	t.Setenv("GINADMIN_ROOT_PASSWORD", "e10adc3949ba59abbe56e057f20f883e")
}

func TestProdSampleValidates(t *testing.T) {
	setProdEnv(t)
	if err := loadProdSample(t); err != nil {
		t.Fatal(err)
	}
	C.PreLoad()
	if err := C.Validate(); err != nil {
		t.Fatalf("prod sample should validate: %v", err)
	}

	checks := map[string][2]string{
		"Debug off":           {boolStr(C.General.Debug), "false"},
		"Swagger off":         {boolStr(C.General.DisableSwagger), "true"},
		"HTTP addr default":   {C.General.HTTP.Addr, ":8040"},
		"DB type default":     {C.Storage.DB.Type, "mysql"},
		"DB DSN from env":     {C.Storage.DB.DSN, "user:pass@tcp(db:3306)/ginadmin?parseTime=True"},
		"cache is redis":      {C.Storage.Cache.Type, "redis"},
		"auth store is redis": {C.Middleware.Auth.Store.Type, "redis"},
		"auth redis inherits": {C.Middleware.Auth.Store.Redis.Addr, "redis:6379"},
		"no default password": {C.General.DefaultLoginPwd, ""},
	}
	for name, c := range checks {
		if c[0] != c[1] {
			t.Errorf("%s: got %q, want %q", name, c[0], c[1])
		}
	}
	if C.Middleware.Auth.SigningKey != strings.Repeat("k", 64) {
		t.Error("JWT key was not read from the environment")
	}
}

func TestProdSampleRequiresSecrets(t *testing.T) {
	for _, name := range []string{"GINADMIN_DB_DSN", "GINADMIN_REDIS_ADDR", "GINADMIN_JWT_KEY", "GINADMIN_ROOT_PASSWORD"} {
		t.Run(name, func(t *testing.T) {
			setProdEnv(t)
			t.Setenv(name, "")
			unsetEnv(t, name)
			err := loadProdSample(t)
			if err == nil || !strings.Contains(err.Error(), name) {
				t.Fatalf("expected an error naming %s, got: %v", name, err)
			}
		})
	}
}

func boolStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

// unsetEnv removes name for the rest of the test; t.Setenv beforehand makes
// the testing package restore the original value afterwards.
func unsetEnv(t *testing.T, name string) {
	t.Helper()
	if err := os.Unsetenv(name); err != nil {
		t.Fatal(err)
	}
}
