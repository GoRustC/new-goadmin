package captchax

import (
	"bytes"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/LyricTian/captcha/store"
)

func testStore(t *testing.T, s store.Store) {
	t.Helper()
	digits := []byte{1, 2, 3, 4}

	s.Set("a", digits)
	if got := s.Get("a", false); !bytes.Equal(got, digits) {
		t.Fatalf("get without clear: %v", got)
	}
	if got := s.Get("a", true); !bytes.Equal(got, digits) {
		t.Fatalf("get with clear: %v", got)
	}
	if got := s.Get("a", false); got != nil {
		t.Fatalf("captcha should be gone after a clearing get: %v", got)
	}
	if got := s.Get("missing", true); got != nil {
		t.Fatalf("missing captcha: %v", got)
	}

	// Many concurrent clearing gets of one captcha: exactly one wins, none hang.
	s.Set("b", digits)
	var wg sync.WaitGroup
	var mu sync.Mutex
	winners := 0
	for range 64 {
		wg.Go(func() {
			if s.Get("b", true) != nil {
				mu.Lock()
				winners++
				mu.Unlock()
			}
		})
	}
	finished := make(chan struct{})
	go func() { wg.Wait(); close(finished) }()
	select {
	case <-finished:
	case <-time.After(10 * time.Second):
		t.Fatal("concurrent gets hung")
	}
	if winners != 1 {
		t.Fatalf("a captcha must be usable exactly once, got %d winners", winners)
	}
}

func TestMemoryStore(t *testing.T) {
	testStore(t, NewMemoryStore(time.Minute))
}

func TestMemoryStoreExpiry(t *testing.T) {
	s := NewMemoryStore(50 * time.Millisecond)
	s.Set("a", []byte{1})
	time.Sleep(100 * time.Millisecond)
	if got := s.Get("a", false); got != nil {
		t.Fatalf("expired captcha returned: %v", got)
	}
	s.Set("b", []byte{1}) // triggers a sweep
	s.mu.Lock()
	n := len(s.items)
	s.mu.Unlock()
	if n != 1 {
		t.Fatalf("expired entries should be swept, %d left", n)
	}
}

// Set REDIS_ADDR to run against a real Redis.
func TestRedisStore(t *testing.T) {
	addr := os.Getenv("REDIS_ADDR")
	if addr == "" {
		t.Skip("REDIS_ADDR not set")
	}
	s := NewRedisStore(RedisConfig{Addr: addr, KeyPrefix: "captchax-test:" + time.Now().Format("150405.000") + ":"}, time.Minute)
	defer s.Close()
	testStore(t, s)
}
