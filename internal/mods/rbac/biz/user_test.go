package biz

import (
	"context"
	"testing"
	"time"

	"github.com/GoRustC/new-goadmin/internal/config"
	"github.com/GoRustC/new-goadmin/pkg/cachex"
)

// A request that read a user's old status just before a freeze committed can
// write it back to the cache after the first invalidation. The delayed second
// delete must remove it, or the frozen user keeps access.
func TestInvalidateUserCacheRemovesLateStaleWrite(t *testing.T) {
	orig := userCacheRedeleteDelay
	userCacheRedeleteDelay = 50 * time.Millisecond
	t.Cleanup(func() { userCacheRedeleteDelay = orig })

	ctx := context.Background()
	cache := cachex.NewMemoryCache(cachex.MemoryConfig{CleanupInterval: time.Minute})
	u := &User{Cache: cache}

	if err := cache.Set(ctx, config.CacheNSForUser, "u1", `{"rids":["old"]}`); err != nil {
		t.Fatal(err)
	}
	if err := u.invalidateUserCache(ctx, "u1"); err != nil {
		t.Fatal(err)
	}
	if ok, _ := cache.Exists(ctx, config.CacheNSForUser, "u1"); ok {
		t.Fatal("entry should be removed immediately")
	}

	// The racing request writes the stale entry back.
	if err := cache.Set(ctx, config.CacheNSForUser, "u1", `{"rids":["old"]}`); err != nil {
		t.Fatal(err)
	}
	time.Sleep(150 * time.Millisecond)
	if ok, _ := cache.Exists(ctx, config.CacheNSForUser, "u1"); ok {
		t.Fatal("stale entry written after invalidation should be removed by the second delete")
	}
}
