package wirex

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/LyricTian/gin-admin/v10/internal/config"
	"github.com/LyricTian/gin-admin/v10/internal/mods"
	"github.com/LyricTian/gin-admin/v10/pkg/cachex"
	"github.com/LyricTian/gin-admin/v10/pkg/gormx"
	"github.com/LyricTian/gin-admin/v10/pkg/jwtx"
	"github.com/LyricTian/gin-admin/v10/pkg/logging"
	"github.com/golang-jwt/jwt/v5"
	"gorm.io/gorm"
)

const minSigningKeyLen = 32

// Keys that were shipped in earlier versions of this project. They are public,
// so any token signed with them can be forged.
var knownDefaultSigningKeys = map[string]struct{}{
	"XnEsT0S@":             {},
	"CG24SDVP8OHPK395GB5G": {},
}

// resolveSigningKeys validates the configured JWT keys. An empty key is only
// tolerated in debug mode, where a random per-run key is generated instead.
func resolveSigningKeys(ctx context.Context, key, oldKey string) (string, string, error) {
	if _, ok := knownDefaultSigningKeys[oldKey]; ok {
		return "", "", errors.New("config Middleware.Auth.OldSigningKey is a publicly known default value, remove it")
	}

	if key == "" {
		if !config.C.IsDebug() {
			return "", "", fmt.Errorf("config Middleware.Auth.SigningKey is required (at least %d bytes)", minSigningKeyLen)
		}
		b := make([]byte, minSigningKeyLen)
		if _, err := rand.Read(b); err != nil {
			return "", "", err
		}
		logging.Context(ctx).Warn("Middleware.Auth.SigningKey is empty, using a random key for this debug run; issued tokens will not survive a restart")
		return hex.EncodeToString(b), oldKey, nil
	}

	if _, ok := knownDefaultSigningKeys[key]; ok {
		return "", "", errors.New("config Middleware.Auth.SigningKey is a publicly known default value, set your own")
	}
	if len(key) < minSigningKeyLen {
		return "", "", fmt.Errorf("config Middleware.Auth.SigningKey must be at least %d bytes", minSigningKeyLen)
	}
	return key, oldKey, nil
}

type Injector struct {
	DB    *gorm.DB
	Cache cachex.Cacher
	Auth  jwtx.Auther
	M     *mods.Mods
}

// It creates a new database connection, and returns a function that closes the connection
func InitDB(ctx context.Context) (*gorm.DB, func(), error) {
	cfg := config.C.Storage.DB

	resolver := make([]gormx.ResolverConfig, len(cfg.Resolver))
	for i, v := range cfg.Resolver {
		resolver[i] = gormx.ResolverConfig{
			DBType:   v.DBType,
			Sources:  v.Sources,
			Replicas: v.Replicas,
			Tables:   v.Tables,
		}
	}

	db, err := gormx.New(gormx.Config{
		Debug:        cfg.Debug,
		PrepareStmt:  cfg.PrepareStmt,
		DBType:       cfg.Type,
		DSN:          cfg.DSN,
		MaxLifetime:  cfg.MaxLifetime,
		MaxIdleTime:  cfg.MaxIdleTime,
		MaxOpenConns: cfg.MaxOpenConns,
		MaxIdleConns: cfg.MaxIdleConns,
		TablePrefix:  cfg.TablePrefix,
		Resolver:     resolver,
	})
	if err != nil {
		return nil, nil, err
	}

	return db, func() {
		sqlDB, err := db.DB()
		if err == nil {
			_ = sqlDB.Close()
		}
	}, nil
}

// It returns a cachex.Cacher instance, a function to close the cache, and an error
func InitCacher(ctx context.Context) (cachex.Cacher, func(), error) {
	cfg := config.C.Storage.Cache

	var cache cachex.Cacher
	switch cfg.Type {
	case "redis":
		cache = cachex.NewRedisCache(cachex.RedisConfig{
			Addr:     cfg.Redis.Addr,
			DB:       cfg.Redis.DB,
			Username: cfg.Redis.Username,
			Password: cfg.Redis.Password,
		}, cachex.WithDelimiter(cfg.Delimiter))
	case "badger":
		cache = cachex.NewBadgerCache(cachex.BadgerConfig{
			Path: cfg.Badger.Path,
		}, cachex.WithDelimiter(cfg.Delimiter))
	default:
		cache = cachex.NewMemoryCache(cachex.MemoryConfig{
			CleanupInterval: time.Second * time.Duration(cfg.Memory.CleanupInterval),
		}, cachex.WithDelimiter(cfg.Delimiter))
	}

	return cache, func() {
		_ = cache.Close(ctx)
	}, nil
}

func InitAuth(ctx context.Context) (jwtx.Auther, func(), error) {
	cfg := config.C.Middleware.Auth
	signingKey, oldSigningKey, err := resolveSigningKeys(ctx, cfg.SigningKey, cfg.OldSigningKey)
	if err != nil {
		return nil, nil, err
	}

	var opts []jwtx.Option
	opts = append(opts, jwtx.SetExpired(cfg.Expired))
	opts = append(opts, jwtx.SetSigningKey(signingKey, oldSigningKey))

	var method jwt.SigningMethod
	switch cfg.SigningMethod {
	case "HS256":
		method = jwt.SigningMethodHS256
	case "HS384":
		method = jwt.SigningMethodHS384
	default:
		method = jwt.SigningMethodHS512
	}
	opts = append(opts, jwtx.SetSigningMethod(method))

	var cache cachex.Cacher
	switch cfg.Store.Type {
	case "redis":
		cache = cachex.NewRedisCache(cachex.RedisConfig{
			Addr:     cfg.Store.Redis.Addr,
			DB:       cfg.Store.Redis.DB,
			Username: cfg.Store.Redis.Username,
			Password: cfg.Store.Redis.Password,
		}, cachex.WithDelimiter(cfg.Store.Delimiter))
	case "badger":
		cache = cachex.NewBadgerCache(cachex.BadgerConfig{
			Path: cfg.Store.Badger.Path,
		}, cachex.WithDelimiter(cfg.Store.Delimiter))
	default:
		cache = cachex.NewMemoryCache(cachex.MemoryConfig{
			CleanupInterval: time.Second * time.Duration(cfg.Store.Memory.CleanupInterval),
		}, cachex.WithDelimiter(cfg.Store.Delimiter))
	}

	auth := jwtx.New(jwtx.NewStoreWithCache(cache), opts...)
	return auth, func() {
		_ = auth.Release(ctx)
	}, nil
}
