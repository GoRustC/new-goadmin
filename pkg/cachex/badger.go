// Package cachex provides a key-value cache with memory, Redis and Badger
// backends.
package cachex

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/dgraph-io/badger/v4"
)

// BadgerConfig configures NewBadgerCache. Path is the database directory.
type BadgerConfig struct {
	Path string
}

// NewBadgerCache returns a cache stored in an embedded Badger database, which
// survives restarts without a separate server.
func NewBadgerCache(cfg BadgerConfig, opts ...Option) Cacher {
	defaultOpts := &options{
		Delimiter: defaultDelimiter,
	}

	for _, o := range opts {
		o(defaultOpts)
	}

	badgerOpts := badger.DefaultOptions(cfg.Path)
	badgerOpts = badgerOpts.WithLoggingLevel(badger.ERROR)
	db, err := badger.Open(badgerOpts)
	if err != nil {
		panic(err)
	}

	return &badgerCache{
		opts: defaultOpts,
		db:   db,
	}
}

type badgerCache struct {
	opts *options
	db   *badger.DB
	// mu serializes GetAndDelete and Incr. Badger lets one process open a
	// directory, so this removes conflicts between them; update still
	// retries conflicts with plain writes such as Set.
	mu sync.Mutex
}

func (a *badgerCache) getKey(ns, key string) string {
	return a.opts.key(ns, key)
}

func (a *badgerCache) Set(ctx context.Context, ns, key, value string, expiration ...time.Duration) error {
	return a.db.Update(func(txn *badger.Txn) error {
		entry := badger.NewEntry([]byte(a.getKey(ns, key)), []byte(value))
		if len(expiration) > 0 {
			entry = entry.WithTTL(expiration[0])
		}
		return txn.SetEntry(entry)
	})
}

func (a *badgerCache) Get(ctx context.Context, ns, key string) (string, bool, error) {
	value := ""
	ok := false
	err := a.db.View(func(txn *badger.Txn) error {
		item, err := txn.Get([]byte(a.getKey(ns, key)))
		if err != nil {
			if errors.Is(err, badger.ErrKeyNotFound) {
				return nil
			}
			return err
		}
		ok = true
		val, err := item.ValueCopy(nil)
		value = string(val)
		return err
	})
	if err != nil {
		return "", false, err
	}
	return value, ok, nil
}

func (a *badgerCache) Exists(ctx context.Context, ns, key string) (bool, error) {
	exists := false
	err := a.db.View(func(txn *badger.Txn) error {
		_, err := txn.Get([]byte(a.getKey(ns, key)))
		if err != nil {
			if errors.Is(err, badger.ErrKeyNotFound) {
				return nil
			}
			return err
		}
		exists = true
		return nil
	})
	return exists, err
}

func (a *badgerCache) Delete(ctx context.Context, ns, key string) error {
	return a.db.Update(func(txn *badger.Txn) error {
		return txn.Delete([]byte(a.getKey(ns, key)))
	})
}

func (a *badgerCache) GetAndDelete(ctx context.Context, ns, key string) (string, bool, error) {
	var (
		value string
		ok    bool
	)
	err := a.update(func(txn *badger.Txn) error {
		value, ok = "", false
		k := []byte(a.getKey(ns, key))
		item, err := txn.Get(k)
		if errors.Is(err, badger.ErrKeyNotFound) {
			return nil
		} else if err != nil {
			return err
		}
		val, err := item.ValueCopy(nil)
		if err != nil {
			return err
		}
		value, ok = string(val), true
		return txn.Delete(k)
	})
	if err != nil {
		return "", false, err
	}
	return value, ok, nil
}

func (a *badgerCache) Incr(ctx context.Context, ns, key string, expiration time.Duration) (int64, error) {
	var n int64
	err := a.update(func(txn *badger.Txn) error {
		k := []byte(a.getKey(ns, key))
		var (
			value string
			ok    bool
		)
		item, err := txn.Get(k)
		switch {
		case errors.Is(err, badger.ErrKeyNotFound):
		case err != nil:
			return err
		default:
			val, err := item.ValueCopy(nil)
			if err != nil {
				return err
			}
			value, ok = string(val), true
		}
		if n, err = parseCounter(value, ok); err != nil {
			return err
		}
		n++
		entry := badger.NewEntry(k, []byte(strconv.FormatInt(n, 10)))
		if expiration > 0 {
			entry = entry.WithTTL(expiration)
		}
		return txn.SetEntry(entry)
	})
	return n, err
}

// update runs fn in a read-write transaction, retrying when a concurrent
// transaction wrote the same key first. Badger transactions are
// serializable, so the read-modify-write in fn is atomic.
func (a *badgerCache) update(fn func(txn *badger.Txn) error) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	var err error
	for range maxConflictRetries {
		if err = a.db.Update(fn); !errors.Is(err, badger.ErrConflict) {
			return err
		}
	}
	return err
}

// maxConflictRetries bounds update's retries under extreme contention.
const maxConflictRetries = 100

func (a *badgerCache) Iterator(ctx context.Context, ns string, fn func(ctx context.Context, key, value string) bool) error {
	return a.db.View(func(txn *badger.Txn) error {
		iterOpts := badger.DefaultIteratorOptions
		iterOpts.Prefix = []byte(a.getKey(ns, ""))
		it := txn.NewIterator(iterOpts)
		defer it.Close()

		it.Rewind()
		for it.Valid() {
			item := it.Item()
			val, err := item.ValueCopy(nil)
			if err != nil {
				return err
			}
			key := string(item.Key())
			if !fn(ctx, strings.TrimPrefix(key, a.getKey(ns, "")), string(val)) {
				break
			}
			it.Next()
		}
		return nil
	})
}

func (a *badgerCache) Close(ctx context.Context) error {
	return a.db.Close()
}
