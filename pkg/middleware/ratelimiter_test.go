package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/GoRustC/new-goadmin/pkg/util"
	"github.com/gin-gonic/gin"
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

func TestRateLimiterMiddleware(t *testing.T) {
	gin.SetMode(gin.TestMode)
	e := gin.New()
	// Simulates the auth middleware: requests with ?user= are signed in.
	e.Use(func(c *gin.Context) {
		if u := c.Query("user"); u != "" {
			c.Request = c.Request.WithContext(util.NewUserID(c.Request.Context(), u))
		}
		c.Next()
	})
	e.Use(RateLimiterWithConfig(RateLimiterConfig{
		Enable:             true,
		Period:             60,
		MaxRequestsPerIP:   2,
		MaxRequestsPerUser: 3,
		StoreType:          "memory",
		MemoryStoreConfig:  RateLimiterMemoryConfig{Expiration: time.Minute, CleanupInterval: time.Minute},
	}))
	e.GET("/x", func(c *gin.Context) { c.Status(http.StatusOK) })

	do := func(remote, query string) int {
		req := httptest.NewRequest(http.MethodGet, "/x"+query, nil)
		req.RemoteAddr = remote
		w := httptest.NewRecorder()
		e.ServeHTTP(w, req)
		return w.Code
	}

	// Anonymous requests are limited per client IP.
	for i := 0; i < 2; i++ {
		if code := do("10.0.0.1:1000", ""); code != http.StatusOK {
			t.Fatalf("anonymous request %d: %d", i+1, code)
		}
	}
	if code := do("10.0.0.1:1001", ""); code != http.StatusTooManyRequests {
		t.Fatalf("third anonymous request from the same IP: %d, want 429", code)
	}
	if code := do("10.0.0.2:1000", ""); code != http.StatusOK {
		t.Fatalf("another IP should have its own budget: %d", code)
	}

	// Signed-in requests are limited per user, regardless of IP.
	for i := 0; i < 3; i++ {
		if code := do("10.0.0.1:1000", "?user=u1"); code != http.StatusOK {
			t.Fatalf("user request %d: %d", i+1, code)
		}
	}
	if code := do("10.0.0.3:1000", "?user=u1"); code != http.StatusTooManyRequests {
		t.Fatalf("fourth request from the same user: %d, want 429", code)
	}
}
