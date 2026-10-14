package config

import (
	"errors"
	"fmt"
	"net"
	"slices"
	"strings"
)

// knownDefaultPassword is MD5("abc-123"), the password shipped in earlier
// versions of this project for the root account and for new or reset users.
const knownDefaultPassword = "6351623c8cef86fefabfa7da046fc619" //nolint:gosec // Rejected, never used to log in.

// minSigningKeyLen matches the check applied to SigningKey at startup.
const minSigningKeyLen = 32

func isBcryptHash(s string) bool {
	return strings.HasPrefix(s, "$2a$") || strings.HasPrefix(s, "$2b$") || strings.HasPrefix(s, "$2y$")
}

// isLoopbackAddr reports whether a listen address binds only to loopback.
// An empty host (":6060") listens on every interface.
func isLoopbackAddr(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil || host == "" {
		return false
	}
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
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

	oneOf("Storage.Cache.Type", c.Storage.Cache.Type, "memory", "redis")
	redisAddr("Storage.Cache", c.Storage.Cache.Type, c.Storage.Cache.Redis.Addr)

	oneOf("Middleware.Auth.Store.Type", c.Middleware.Auth.Store.Type, "memory", "redis")
	redisAddr("Middleware.Auth.Store", c.Middleware.Auth.Store.Type, c.Middleware.Auth.Store.Redis.Addr)
	check(c.Middleware.Auth.Expired > 0, "Middleware.Auth.Expired must be greater than 0")

	if c.Middleware.RateLimiter.Enable {
		oneOf("Middleware.RateLimiter.Store.Type", c.Middleware.RateLimiter.Store.Type, "memory", "redis")
		redisAddr("Middleware.RateLimiter.Store", c.Middleware.RateLimiter.Store.Type, c.Middleware.RateLimiter.Store.Redis.Addr)
		check(c.Middleware.RateLimiter.Period > 0, "Middleware.RateLimiter.Period must be greater than 0")
		// A limit of 0 would silently disable that half of the limiter.
		check(c.Middleware.RateLimiter.MaxRequestsPerIP > 0, "Middleware.RateLimiter.MaxRequestsPerIP must be greater than 0")
		check(c.Middleware.RateLimiter.MaxRequestsPerUser > 0, "Middleware.RateLimiter.MaxRequestsPerUser must be greater than 0")
	}

	if !c.Middleware.Casbin.Disable {
		check(c.Middleware.Casbin.ModelFile != "", "Middleware.Casbin.ModelFile is required")
	}

	check(c.Middleware.CopyBody.MaxContentLen > 0, "Middleware.CopyBody.MaxContentLen must be greater than 0")

	oneOf("Util.Captcha.CacheType", c.Util.Captcha.CacheType, "memory", "redis")
	redisAddr("Util.Captcha", c.Util.Captcha.CacheType, c.Util.Captcha.Redis.Addr)

	if k := c.Middleware.Auth.OldSigningKey; k != "" {
		check(len(k) >= minSigningKeyLen,
			"Middleware.Auth.OldSigningKey must be at least %d bytes, like SigningKey", minSigningKeyLen)
	}

	if addr := c.General.MetricsAddr; addr != "" {
		_, port, err := net.SplitHostPort(addr)
		check(err == nil && port != "", "General.MetricsAddr must be host:port, e.g. 127.0.0.1:8041; got %q", addr)
		check(addr != c.General.HTTP.Addr, "General.MetricsAddr must differ from General.HTTP.Addr")
	}

	// Development conveniences are refused outside debug mode.
	if !c.IsDebug() {
		check(c.General.Root.Password != knownDefaultPassword,
			"General.Root.Password is a publicly known default value, set your own")
		check(c.General.Root.Password == "" || isBcryptHash(c.General.Root.Password),
			"General.Root.Password must be a bcrypt hash outside debug mode (generate one with: ginadmin hash-password), or empty to disable root")
		check(c.General.DefaultLoginPwd != knownDefaultPassword,
			"General.DefaultLoginPwd is a publicly known default value, set your own or leave it empty")
		check(!c.Middleware.Auth.Disable, "Middleware.Auth.Disable must not be true outside debug mode")
		check(!c.Middleware.Casbin.Disable, "Middleware.Casbin.Disable must not be true outside debug mode")
		if addr := c.General.PprofAddr; addr != "" {
			check(isLoopbackAddr(addr),
				"General.PprofAddr must listen on a loopback address outside debug mode, e.g. 127.0.0.1:6060; got %q", addr)
		}
	}

	return errors.Join(errs...)
}
