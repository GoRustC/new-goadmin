package cachex

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestBadgerCache(t *testing.T) {
	assert := assert.New(t)

	cache := NewBadgerCache(BadgerConfig{
		Path: "./tmp/badger",
	})

	ctx := context.Background()
	err := cache.Set(ctx, "tt", "foo", "bar")
	assert.Nil(err)

	val, exists, err := cache.Get(ctx, "tt", "foo")
	assert.Nil(err)
	assert.True(exists)
	assert.Equal("bar", val)

	err = cache.Delete(ctx, "tt", "foo")
	assert.Nil(err)

	val, exists, err = cache.Get(ctx, "tt", "foo")
	assert.Nil(err)
	assert.False(exists)
	assert.Equal("", val)

	tmap := make(map[string]bool)
	for i := 0; i < 10; i++ {
		key := fmt.Sprintf("foo%d", i)
		err = cache.Set(ctx, "tt", key, "bar")
		assert.Nil(err)
		tmap[key] = true

		err = cache.Set(ctx, "ff", key, "bar")
		assert.Nil(err)
	}

	err = cache.Iterator(ctx, "tt", func(ctx context.Context, key, value string) bool {
		assert.True(tmap[key])
		assert.Equal("bar", value)
		t.Log(key, value)
		return true
	})
	assert.Nil(err)

	err = cache.Close(ctx)
	assert.Nil(err)
}

func TestMemoryCacheKeyPrefix(t *testing.T) {
	ctx := context.Background()
	a := NewMemoryCache(MemoryConfig{CleanupInterval: time.Minute}, WithKeyPrefix("app1:"))
	if err := a.Set(ctx, "user", "u1", "v"); err != nil {
		t.Fatal(err)
	}
	mc := a.(*memCache)
	if _, ok := mc.cache.Get("app1:user:u1"); !ok {
		t.Fatalf("key not stored under the prefix; keys: %v", mc.cache.Items())
	}
	var seen []string
	_ = a.Iterator(ctx, "user", func(_ context.Context, key, _ string) bool {
		seen = append(seen, key)
		return true
	})
	if len(seen) != 1 || seen[0] != "u1" {
		t.Fatalf("iterator should strip the prefix, got %v", seen)
	}
}
