// Package cachex provides a key-value cache with memory and Redis backends.
package cachex

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/patrickmn/go-cache"
)

// Cacher stores string values under a namespace and a key. Get reports
// whether the key exists; Iterator calls fn for each entry of a namespace
// until fn returns false. Expiration is optional; zero or none means no
// expiry.
//
// GetAndDelete and Incr are atomic: concurrent callers never both read the
// same value, and no increment is lost.
type Cacher interface {
	Set(ctx context.Context, ns, key, value string, expiration ...time.Duration) error
	Get(ctx context.Context, ns, key string) (string, bool, error)
	// GetAndDelete returns the value of key and removes it.
	GetAndDelete(ctx context.Context, ns, key string) (string, bool, error)
	// Incr adds 1 to the integer stored at key (a missing key counts as 0),
	// sets the key to expire after expiration (0 means never) and returns
	// the new value. It fails if the stored value is not an integer.
	Incr(ctx context.Context, ns, key string, expiration time.Duration) (int64, error)
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

// Option configures a cache.
type Option func(*options)

// WithDelimiter sets the separator between namespace and key. The default
// is ":".
func WithDelimiter(delimiter string) Option {
	return func(o *options) {
		o.Delimiter = delimiter
	}
}

// MemoryConfig configures NewMemoryCache.
type MemoryConfig struct {
	CleanupInterval time.Duration
}

// NewMemoryCache returns a cache held in process memory, swept for expired
// entries every CleanupInterval.
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
	mu    sync.Mutex // makes GetAndDelete and Incr atomic
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
	a.mu.Lock()
	defer a.mu.Unlock()
	value, ok, err := a.Get(ctx, ns, key)
	if err != nil || !ok {
		return "", false, err
	}
	a.cache.Delete(a.getKey(ns, key))
	return value, true, nil
}

func (a *memCache) Incr(ctx context.Context, ns, key string, expiration time.Duration) (int64, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	value, ok, err := a.Get(ctx, ns, key)
	if err != nil {
		return 0, err
	}
	n, err := parseCounter(value, ok)
	if err != nil {
		return 0, err
	}
	n++
	a.cache.Set(a.getKey(ns, key), strconv.FormatInt(n, 10), expiration)
	return n, nil
}

// parseCounter returns the integer stored in a counter, 0 if it is missing.
func parseCounter(value string, ok bool) (int64, error) {
	if !ok {
		return 0, nil
	}
	n, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("cachex: value is not an integer: %w", err)
	}
	return n, nil
}

func (a *memCache) Iterator(ctx context.Context, ns string, fn func(ctx context.Context, key, value string) bool) error {
	for k, v := range a.cache.Items() {
		if after, ok := strings.CutPrefix(k, a.getKey(ns, "")); ok {
			if !fn(ctx, after, v.Object.(string)) {
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
