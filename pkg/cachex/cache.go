package cachex

import (
	"context"
	"strings"
	"time"

	"github.com/patrickmn/go-cache"
)

// Cacher is the interface that wraps the basic Get, Set, and Delete methods.
type Cacher interface {
	Set(ctx context.Context, ns, key, value string, expiration ...time.Duration) error
	Get(ctx context.Context, ns, key string) (string, bool, error)
	GetAndDelete(ctx context.Context, ns, key string) (string, bool, error)
	Exists(ctx context.Context, ns, key string) (bool, error)
	Delete(ctx context.Context, ns, key string) error
	Iterator(ctx context.Context, ns string, fn func(ctx context.Context, key, value string) bool) error
	Close(ctx context.Context) error
}

var defaultDelimiter = ":"

type options struct {
	Delimiter string
	KeyPrefix string
}

// key builds the stored key: KeyPrefix, then the namespace, the delimiter and
// the key.
func (o *options) key(ns, key string) string {
	return o.KeyPrefix + ns + o.Delimiter + key
}

// WithKeyPrefix puts prefix in front of every key, so several applications
// or environments can share one Redis database without colliding.
func WithKeyPrefix(prefix string) Option {
	return func(o *options) {
		o.KeyPrefix = prefix
	}
}

type Option func(*options)

func WithDelimiter(delimiter string) Option {
	return func(o *options) {
		o.Delimiter = delimiter
	}
}

type MemoryConfig struct {
	CleanupInterval time.Duration
}

func NewMemoryCache(cfg MemoryConfig, opts ...Option) Cacher {
	defaultOpts := &options{
		Delimiter: defaultDelimiter,
	}

	for _, o := range opts {
		o(defaultOpts)
	}

	return &memCache{
		opts:  defaultOpts,
		cache: cache.New(0, cfg.CleanupInterval),
	}
}

type memCache struct {
	opts  *options
	cache *cache.Cache
}

func (a *memCache) getKey(ns, key string) string {
	return a.opts.key(ns, key)
}

func (a *memCache) Set(ctx context.Context, ns, key, value string, expiration ...time.Duration) error {
	var exp time.Duration
	if len(expiration) > 0 {
		exp = expiration[0]
	}

	a.cache.Set(a.getKey(ns, key), value, exp)
	return nil
}

func (a *memCache) Get(ctx context.Context, ns, key string) (string, bool, error) {
	val, ok := a.cache.Get(a.getKey(ns, key))
	if !ok {
		return "", false, nil
	}
	return val.(string), ok, nil
}

func (a *memCache) Exists(ctx context.Context, ns, key string) (bool, error) {
	_, ok := a.cache.Get(a.getKey(ns, key))
	return ok, nil
}

func (a *memCache) Delete(ctx context.Context, ns, key string) error {
	a.cache.Delete(a.getKey(ns, key))
	return nil
}

func (a *memCache) GetAndDelete(ctx context.Context, ns, key string) (string, bool, error) {
	value, ok, err := a.Get(ctx, ns, key)
	if err != nil {
		return "", false, err
	} else if !ok {
		return "", false, nil
	}

	a.cache.Delete(a.getKey(ns, key))
	return value, true, nil
}

func (a *memCache) Iterator(ctx context.Context, ns string, fn func(ctx context.Context, key, value string) bool) error {
	for k, v := range a.cache.Items() {
		if strings.HasPrefix(k, a.getKey(ns, "")) {
			if !fn(ctx, strings.TrimPrefix(k, a.getKey(ns, "")), v.Object.(string)) {
				break
			}
		}
	}
	return nil
}

func (a *memCache) Close(ctx context.Context) error {
	a.cache.Flush()
	return nil
}
