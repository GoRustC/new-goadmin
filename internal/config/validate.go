package config

import (
	"errors"
	"fmt"
	"slices"
)

// knownDefaultPassword is MD5("abc-123"), the password shipped in earlier
// versions of this project for the root account and for new or reset users.
const knownDefaultPassword = "6351623c8cef86fefabfa7da046fc619"

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
