package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/GoRustC/new-goadmin/pkg/cachex"
)

func TestUnauthorizedLimiter(t *testing.T) {
	gin.SetMode(gin.TestMode)
	logged := 0
	e := gin.New()
	e.Use(UnauthorizedLimiterWithConfig(UnauthorizedLimiterConfig{
		Enable:              true,
		AllowedPathPrefixes: []string{"/api/"},
		Period:              time.Minute,
		MaxFailures:         3,
		Counter:             cachex.NewMemoryCache(cachex.MemoryConfig{CleanupInterval: time.Minute}),
	}))
	e.Use(func(c *gin.Context) { logged++ }) // stands in for the request logger
	e.GET("/api/data", func(c *gin.Context) {
		if c.GetHeader("Authorization") != "good" {
			c.AbortWithStatus(http.StatusUnauthorized)
			return
		}
		c.Status(http.StatusOK)
	})

	get := func(ip, token string) int {
		req := httptest.NewRequest(http.MethodGet, "/api/data", nil)
		req.RemoteAddr = ip + ":1234"
		req.Header.Set("Authorization", token)
		w := httptest.NewRecorder()
		e.ServeHTTP(w, req)
		return w.Code
	}

	// Authenticated requests are never counted.
	for range 10 {
		if code := get("10.0.0.1", "good"); code != http.StatusOK {
			t.Fatalf("authenticated request: got %d", code)
		}
	}
	for i := range 3 {
		if code := get("10.0.0.1", "bad"); code != http.StatusUnauthorized {
			t.Fatalf("failure %d: got %d, want 401", i+1, code)
		}
	}
	logged = 0
	for range 5 {
		if code := get("10.0.0.1", "bad"); code != http.StatusTooManyRequests {
			t.Fatalf("after the limit: got %d, want 429", code)
		}
	}
	if logged != 0 {
		t.Errorf("refused requests reached the logger %d times", logged)
	}
	// Other clients are unaffected.
	if code := get("10.0.0.2", "bad"); code != http.StatusUnauthorized {
		t.Errorf("another IP: got %d, want 401", code)
	}
}
