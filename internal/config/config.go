package config

import (
	"fmt"
	"strings"

	"github.com/GoRustC/new-goadmin/pkg/encoding/json"
	"github.com/GoRustC/new-goadmin/pkg/logging"
)

type Config struct {
	Logger     logging.LoggerConfig
	General    General
	Storage    Storage
	Middleware Middleware
	Util       Util
	Dictionary Dictionary
}

type General struct {
	AppName            string `default:"ginadmin"`
	Version            string `default:"v10.1.0"`
	Debug              bool
	PprofAddr          string
	DisableSwagger     bool `default:"true"`
	DisablePrintConfig bool
	DefaultLoginPwd    string // Password (MD5 hash, as sent by the frontend) for new users without one and for resets; empty disables both
	WorkDir            string // From command arguments
	MenuFile           string // From schema.Menus (JSON/YAML)
	DenyOperateMenu    bool
	HTTP               struct {
		Addr            string `default:":8040"`
		ShutdownTimeout int    `default:"10"` // seconds
		ReadTimeout     int    `default:"60"` // seconds
		WriteTimeout    int    `default:"60"` // seconds
		IdleTimeout     int    `default:"10"` // seconds
		// Limits on request headers, against slow or oversized header attacks.
		ReadHeaderTimeout int `default:"10"`      // seconds
		MaxHeaderBytes    int `default:"1048576"` // bytes
		CertFile          string
		KeyFile           string
		// Proxies (IPs or CIDRs) allowed to set X-Forwarded-For / X-Real-IP.
		// Empty trusts none, so the client IP is always the TCP peer address.
		TrustedProxies []string
	}
	// Locks an account name after repeated failed logins. Counting applies
	// to every submitted name, existing or not, so a lock reveals nothing.
	LoginLock struct {
		MaxFailures int `default:"5"`  // failures before locking; 0 disables
		LockMinutes int `default:"15"` // lock duration and failure window
	}
	Root struct {
		ID       string `default:"root"`
		Username string `default:"admin"`
		Password string // MD5 hash as sent by the frontend, or a bcrypt hash of it; empty disables root login
		Name     string `default:"Admin"`
	}
}

type Storage struct {
	Cache struct {
		Type      string `default:"memory"` // memory/badger/redis
		Delimiter string `default:":"`      // delimiter for key
		Memory    struct {
			CleanupInterval int `default:"60"` // seconds
		}
		Badger struct {
			Path string `default:"data/cache"`
		}
		Redis struct {
			Addr     string
			Username string
			Password string
			DB       int
		}
	}
	DB struct {
		Debug        bool
		Type         string `default:"sqlite3"`          // sqlite3/mysql/postgres
		DSN          string `default:"data/ginadmin.db"` // database source name
		MaxLifetime  int    `default:"86400"`            // seconds
		MaxIdleTime  int    `default:"3600"`             // seconds
		MaxOpenConns int    `default:"100"`              // connections
		MaxIdleConns int    `default:"50"`               // connections
		TablePrefix  string `default:""`
		AutoMigrate  bool
		PrepareStmt  bool
		Resolver     []struct {
			DBType   string   // sqlite3/mysql/postgres
			Sources  []string // DSN
			Replicas []string // DSN
			Tables   []string
		}
	}
}

type Util struct {
	Captcha struct {
		Length    int    `default:"4"`
		Width     int    `default:"400"`
		Height    int    `default:"160"`
		CacheType string `default:"memory"` // memory/redis
		Redis     struct {
			Addr      string
			Username  string
			Password  string
			DB        int
			KeyPrefix string `default:"captcha:"`
		}
	}
}

type Dictionary struct {
	UserCacheExp int `default:"4"` // hours
	// Delete logger table rows older than this many days, checked hourly.
	// 0 keeps everything; the table otherwise grows with every request.
	LoggerRetentionDays int
}

func (c *Config) IsDebug() bool {
	return c.General.Debug
}

// String returns the configuration as indented JSON with secrets masked, so it
// is safe to print or log.
func (c *Config) String() string {
	b, err := json.Marshal(c)
	if err != nil {
		panic("Failed to marshal config: " + err.Error())
	}

	var v any
	if err := json.Unmarshal(b, &v); err != nil {
		panic("Failed to unmarshal config: " + err.Error())
	}
	redactSecrets(v)

	out, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		panic("Failed to marshal config: " + err.Error())
	}
	return string(out)
}

const redactedValue = "******"

// Lowercased substrings of config keys whose values must never be printed.
// DSN, Sources and Replicas are connection strings that embed credentials.
var sensitiveKeyParts = []string{"password", "pwd", "secret", "signingkey", "dsn", "sources", "replicas"}

func isSensitiveKey(key string) bool {
	k := strings.ToLower(key)
	for _, part := range sensitiveKeyParts {
		if strings.Contains(k, part) {
			return true
		}
	}
	return false
}

func redactSecrets(v any) {
	switch t := v.(type) {
	case map[string]any:
		for k, val := range t {
			if isSensitiveKey(k) {
				t[k] = maskValue(val)
				continue
			}
			redactSecrets(val)
		}
	case []any:
		for _, item := range t {
			redactSecrets(item)
		}
	}
}

func maskValue(v any) any {
	switch t := v.(type) {
	case string:
		if t == "" {
			return t
		}
		return redactedValue
	case []any:
		for i, item := range t {
			t[i] = maskValue(item)
		}
		return t
	default:
		return v
	}
}

func (c *Config) PreLoad() {
	if addr := c.Storage.Cache.Redis.Addr; addr != "" {
		username := c.Storage.Cache.Redis.Username
		password := c.Storage.Cache.Redis.Password
		if c.Util.Captcha.CacheType == "redis" &&
			c.Util.Captcha.Redis.Addr == "" {
			c.Util.Captcha.Redis.Addr = addr
			c.Util.Captcha.Redis.Username = username
			c.Util.Captcha.Redis.Password = password
		}
		if c.Middleware.RateLimiter.Store.Type == "redis" &&
			c.Middleware.RateLimiter.Store.Redis.Addr == "" {
			c.Middleware.RateLimiter.Store.Redis.Addr = addr
			c.Middleware.RateLimiter.Store.Redis.Username = username
			c.Middleware.RateLimiter.Store.Redis.Password = password
		}
		if c.Middleware.Auth.Store.Type == "redis" &&
			c.Middleware.Auth.Store.Redis.Addr == "" {
			c.Middleware.Auth.Store.Redis.Addr = addr
			c.Middleware.Auth.Store.Redis.Username = username
			c.Middleware.Auth.Store.Redis.Password = password
		}
	}
}

func (c *Config) Print() {
	if c.General.DisablePrintConfig {
		return
	}
	fmt.Println("// ----------------------- Load configurations start ------------------------")
	fmt.Println(c.String())
	fmt.Println("// ----------------------- Load configurations end --------------------------")
}
