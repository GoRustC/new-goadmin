// Package captchax provides captcha stores for github.com/LyricTian/captcha.
//
// The library's built-in memory store deadlocks when the same captcha is
// verified concurrently: its remove method can panic while holding the write
// lock and never unlocks, after which every captcha request blocks forever.
// Always install one of these stores with captcha.SetCustomStore instead.
package captchax

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/LyricTian/captcha/store"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

var _ store.Store = (*MemoryStore)(nil)
var _ store.Store = (*RedisStore)(nil)

type memoryEntry struct {
	digits  []byte
	expires time.Time
}

// MemoryStore keeps captchas in process memory. It is safe for concurrent use.
// Captchas are only visible to the instance that issued them.
type MemoryStore struct {
	mu         sync.Mutex
	items      map[string]memoryEntry
	expiration time.Duration
	lastGC     time.Time
}

// NewMemoryStore returns a store whose captchas expire after expiration.
func NewMemoryStore(expiration time.Duration) *MemoryStore {
	return &MemoryStore{
		items:      make(map[string]memoryEntry),
		expiration: expiration,
		lastGC:     time.Now(),
	}
}

// Set stores the digits of captcha id.
func (s *MemoryStore) Set(id string, digits []byte) {
	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.items[id] = memoryEntry{digits: append([]byte(nil), digits...), expires: now.Add(s.expiration)}
	// Sweep expired entries at most once per expiration period.
	if now.Sub(s.lastGC) >= s.expiration {
		for k, e := range s.items {
			if now.After(e.expires) {
				delete(s.items, k)
			}
		}
		s.lastGC = now
	}
}

// Get returns the digits of captcha id, or nil if it does not exist or has
// expired. remove deletes the captcha.
func (s *MemoryStore) Get(id string, remove bool) []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.items[id]
	if !ok {
		return nil
	}
	expired := time.Now().After(e.expires)
	if remove || expired {
		delete(s.items, id)
	}
	if expired {
		return nil
	}
	return e.digits
}

// RedisConfig configures RedisStore.
type RedisConfig struct {
	Addr      string
	Username  string
	Password  string
	DB        int
	KeyPrefix string
}

// RedisStore keeps captchas in Redis, so any instance can verify a captcha
// issued by another. Verification reads and deletes atomically with GETDEL,
// so a captcha can be used once even under concurrent requests.
type RedisStore struct {
	cli        *redis.Client
	prefix     string
	expiration time.Duration
	timeout    time.Duration
}

// NewRedisStore returns a store whose captchas expire after expiration.
func NewRedisStore(cfg RedisConfig, expiration time.Duration) *RedisStore {
	return &RedisStore{
		cli: redis.NewClient(&redis.Options{
			Addr:     cfg.Addr,
			Username: cfg.Username,
			Password: cfg.Password,
			DB:       cfg.DB,
		}),
		prefix:     cfg.KeyPrefix,
		expiration: expiration,
		timeout:    3 * time.Second,
	}
}

// Set stores the digits of captcha id. The store interface has no error
// return, so failures are logged and a failed Get reports the captcha as
// missing, which rejects the login.
func (s *RedisStore) Set(id string, digits []byte) {
	ctx, cancel := context.WithTimeout(context.Background(), s.timeout)
	defer cancel()
	if err := s.cli.Set(ctx, s.prefix+id, digits, s.expiration).Err(); err != nil {
		zap.L().Error("Failed to store captcha", zap.Error(err))
	}
}

// Get returns the digits of captcha id, or nil if it does not exist, has
// expired or cannot be read. remove deletes the captcha atomically.
func (s *RedisStore) Get(id string, remove bool) []byte {
	ctx, cancel := context.WithTimeout(context.Background(), s.timeout)
	defer cancel()
	var (
		val []byte
		err error
	)
	if remove {
		val, err = s.cli.GetDel(ctx, s.prefix+id).Bytes()
	} else {
		val, err = s.cli.Get(ctx, s.prefix+id).Bytes()
	}
	if err != nil {
		if !errors.Is(err, redis.Nil) {
			zap.L().Error("Failed to read captcha", zap.Error(err))
		}
		return nil
	}
	return val
}

// Close releases the Redis connection pool.
func (s *RedisStore) Close() error {
	return s.cli.Close()
}
