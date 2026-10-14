package cachex

import (
	"context"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// backends returns every cache implementation available in this run. Redis
// is included when REDIS_ADDR is set.
func backends(t *testing.T) map[string]Cacher {
	t.Helper()
	prefix := WithKeyPrefix("atomic-test:" + time.Now().Format("150405.000000") + ":")
	m := map[string]Cacher{
		"memory": NewMemoryCache(MemoryConfig{CleanupInterval: time.Minute}, prefix),
		"badger": NewBadgerCache(BadgerConfig{Path: t.TempDir()}, prefix),
	}
	if addr := os.Getenv("REDIS_ADDR"); addr != "" {
		m["redis"] = NewRedisCache(RedisConfig{Addr: addr, DB: 1}, prefix)
	}
	for _, c := range m {
		t.Cleanup(func() { _ = c.Close(context.Background()) })
	}
	return m
}

func TestIncrIsAtomic(t *testing.T) {
	const workers, perWorker = 16, 50
	for name, c := range backends(t) {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			var wg sync.WaitGroup
			for range workers {
				wg.Go(func() {
					for range perWorker {
						if _, err := c.Incr(ctx, "n", "k", time.Minute); err != nil {
							t.Error(err)
							return
						}
					}
				})
			}
			wg.Wait()

			got, ok, err := c.Get(ctx, "n", "k")
			if err != nil || !ok || got != "800" {
				t.Fatalf("after %d increments the value is %q (found %v, err %v)", workers*perWorker, got, ok, err)
			}
			if n, err := c.Incr(ctx, "n", "k", time.Minute); err != nil || n != workers*perWorker+1 {
				t.Fatalf("Incr returned %d, %v; want %d", n, err, workers*perWorker+1)
			}
		})
	}
}

func TestIncrExpiresAndRejectsNonIntegers(t *testing.T) {
	for name, c := range backends(t) {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			if n, err := c.Incr(ctx, "n", "short", time.Second); err != nil || n != 1 {
				t.Fatalf("first Incr = %d, %v; want 1", n, err)
			}
			deadline := time.Now().Add(5 * time.Second)
			for {
				if ok, _ := c.Exists(ctx, "n", "short"); !ok {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("counter did not expire")
				}
				time.Sleep(100 * time.Millisecond)
			}

			if err := c.Set(ctx, "n", "text", "abc"); err != nil {
				t.Fatal(err)
			}
			if _, err := c.Incr(ctx, "n", "text", time.Minute); err == nil {
				t.Fatal("Incr on a non-integer value should fail")
			}
		})
	}
}

func TestGetAndDeleteIsAtomic(t *testing.T) {
	for name, c := range backends(t) {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			for round := range 20 {
				if err := c.Set(ctx, "once", "k", "v", time.Minute); err != nil {
					t.Fatal(err)
				}
				var winners atomic.Int32
				var wg sync.WaitGroup
				for range 8 {
					wg.Go(func() {
						v, ok, err := c.GetAndDelete(ctx, "once", "k")
						if err != nil {
							t.Error(err)
						} else if ok && v == "v" {
							winners.Add(1)
						}
					})
				}
				wg.Wait()
				if n := winners.Load(); n != 1 {
					t.Fatalf("round %d: %d callers got the value, want exactly 1", round, n)
				}
			}
		})
	}
}
