package middleware

import (
	"context"
	"testing"
	"time"
)

func TestRateLimiterMemoryStore(t *testing.T) {
	ctx := context.Background()
	store := NewRateLimiterMemoryStore(RateLimiterMemoryConfig{
		Expiration:      time.Minute,
		CleanupInterval: time.Minute,
	})

	const (
		period      = time.Second
		maxRequests = 10
	)

	// The first maxRequests requests in a period pass, the next one is limited.
	for i := 0; i < maxRequests; i++ {
		if ok, _ := store.Allow(ctx, "ip", period, maxRequests); !ok {
			t.Fatalf("request %d should be allowed", i+1)
		}
	}
	if ok, _ := store.Allow(ctx, "ip", period, maxRequests); ok {
		t.Fatal("request over the limit should be denied")
	}

	// Tokens refill at maxRequests per period: after a fifth of the period
	// about two requests are available again. The old implementation refilled
	// only one token per whole period and denied these.
	time.Sleep(period / 5)
	for i := 0; i < 2; i++ {
		if ok, _ := store.Allow(ctx, "ip", period, maxRequests); !ok {
			t.Fatalf("request %d after partial refill should be allowed", i+1)
		}
	}

	// Identifiers are limited independently.
	if ok, _ := store.Allow(ctx, "other-ip", period, maxRequests); !ok {
		t.Fatal("a different identifier should have its own budget")
	}
}
