package config

import (
	"errors"
	"fmt"
	"os"
	"regexp"
	"slices"
)

// knownDefaultPassword is MD5("abc-123"), the password shipped in earlier
// versions of this project for the root account and for new or reset users.
const knownDefaultPassword = "6351623c8cef86fefabfa7da046fc619"

// envRefPattern matches ${NAME} and ${NAME:-default}. The bare $NAME form is
// deliberately not supported so values such as bcrypt hashes stay untouched.
var envRefPattern = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)(?::-([^}]*))?\}`)

// expandEnv replaces ${NAME} and ${NAME:-default} references with environment
// variable values. A reference to an unset variable without a default is an
// error, so a missing secret fails loudly instead of becoming an empty string.
func expandEnv(buf []byte) ([]byte, error) {
	var missing []string
	out := envRefPattern.ReplaceAllFunc(buf, func(ref []byte) []byte {
		// Group 1 is the name, group 2 the default; index -1 means no default.
		idx := envRefPattern.FindSubmatchIndex(ref)
		name := string(ref[idx[2]:idx[3]])
		if v, ok := os.LookupEnv(name); ok {
			return []byte(v)
		}
		if idx[4] >= 0 {
			return ref[idx[4]:idx[5]]
		}
		missing = append(missing, name)
		return ref
	})
	if len(missing) > 0 {
		return nil, fmt.Errorf("environment variables not set: %v", missing)
	}
	return out, nil
}

// Validate checks the loaded configuration and reports every problem found.
func (c *Config) Validate() error {
	var errs []error
	check := func(ok bool, format string, args ...any) {
		if !ok {
			errs = append(errs, fmt.Errorf(format, args...))
		}
	}
	oneOf := func(name, value string, allowed ...string) {
		check(slices.Contains(allowed, value), "%s must be one of %v, got %q", name, allowed, value)
	}
	redisAddr := func(name, storeType, addr string) {
		check(storeType != "redis" || addr != "", "%s.Redis.Addr is required when the type is redis", name)
	}

	check(c.General.HTTP.Addr != "", "General.HTTP.Addr is required")
	check((c.General.HTTP.CertFile == "") == (c.General.HTTP.KeyFile == ""),
		"General.HTTP.CertFile and General.HTTP.KeyFile must be set together")

	oneOf("Storage.DB.Type", c.Storage.DB.Type, "sqlite3", "mysql", "postgres")
	check(c.Storage.DB.DSN != "", "Storage.DB.DSN is required")

	oneOf("Storage.Cache.Type", c.Storage.Cache.Type, "memory", "badger", "redis")
	redisAddr("Storage.Cache", c.Storage.Cache.Type, c.Storage.Cache.Redis.Addr)

	oneOf("Middleware.Auth.Store.Type", c.Middleware.Auth.Store.Type, "memory", "badger", "redis")
	redisAddr("Middleware.Auth.Store", c.Middleware.Auth.Store.Type, c.Middleware.Auth.Store.Redis.Addr)
	check(c.Middleware.Auth.Expired > 0, "Middleware.Auth.Expired must be greater than 0")

	if c.Middleware.RateLimiter.Enable {
		oneOf("Middleware.RateLimiter.Store.Type", c.Middleware.RateLimiter.Store.Type, "memory", "redis")
		redisAddr("Middleware.RateLimiter.Store", c.Middleware.RateLimiter.Store.Type, c.Middleware.RateLimiter.Store.Redis.Addr)
		check(c.Middleware.RateLimiter.Period > 0, "Middleware.RateLimiter.Period must be greater than 0")
	}

	if !c.Middleware.Casbin.Disable {
		check(c.Middleware.Casbin.AutoLoadInterval > 0, "Middleware.Casbin.AutoLoadInterval must be greater than 0")
		check(c.Middleware.Casbin.LoadThread > 0, "Middleware.Casbin.LoadThread must be greater than 0")
		check(c.Middleware.Casbin.ModelFile != "", "Middleware.Casbin.ModelFile is required")
	}

	check(c.Middleware.CopyBody.MaxContentLen > 0, "Middleware.CopyBody.MaxContentLen must be greater than 0")

	oneOf("Util.Captcha.CacheType", c.Util.Captcha.CacheType, "memory", "redis")
	redisAddr("Util.Captcha", c.Util.Captcha.CacheType, c.Util.Captcha.Redis.Addr)

	// Publicly known passwords are tolerated only for local development.
	if !c.IsDebug() {
		check(c.General.Root.Password != knownDefaultPassword,
			"General.Root.Password is a publicly known default value, set your own")
		check(c.General.DefaultLoginPwd != knownDefaultPassword,
			"General.DefaultLoginPwd is a publicly known default value, set your own or leave it empty")
		check(!c.Middleware.Auth.Disable, "Middleware.Auth.Disable must not be true outside debug mode")
	}

	return errors.Join(errs...)
}
