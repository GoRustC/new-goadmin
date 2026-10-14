// Package cachex provides a key-value cache with memory, Redis and Badger
// backends.
package cachex

import (
	"context"
	"errors"
	"strings"
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
	b, err := a.Exists(ctx, ns, key)
	if err != nil {
		return err
	} else if !b {
		return nil
	}

	return a.db.Update(func(txn *badger.Txn) error {
		return txn.Delete([]byte(a.getKey(ns, key)))
	})
}

func (a *badgerCache) GetAndDelete(ctx context.Context, ns, key string) (string, bool, error) {
	value, ok, err := a.Get(ctx, ns, key)
	if err != nil {
		return "", false, err
	} else if !ok {
		return "", false, nil
	}

	err = a.db.Update(func(txn *badger.Txn) error {
		return txn.Delete([]byte(a.getKey(ns, key)))
	})
	if err != nil {
		return "", false, err
	}

	return value, true, nil
}

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
