package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestTraceIDValidation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	e := gin.New()
	e.Use(TraceWithConfig(TraceConfig{RequestHeaderKey: "X-Request-Id", ResponseTraceKey: "X-Trace-Id"}))
	e.GET("/x", func(c *gin.Context) { c.Status(http.StatusOK) })

	tests := []struct {
		name, in string
		keep     bool
	}{
		{"plain id is kept", "req-123_ABC.4", true},
		{"64 chars is kept", strings.Repeat("a", 64), true},
		{"65 chars is replaced", strings.Repeat("a", 65), false},
		{"spaces are replaced", "has space", false},
		{"non-ascii is replaced", "ид", false},
		{"empty is generated", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/x", nil)
			if tt.in != "" {
				req.Header.Set("X-Request-Id", tt.in)
			}
			w := httptest.NewRecorder()
			e.ServeHTTP(w, req)
			got := w.Header().Get("X-Trace-Id")
			if tt.keep && got != tt.in {
				t.Fatalf("got %q, want the caller's id", got)
			}
			if !tt.keep && (got == tt.in || !strings.HasPrefix(got, "TRACE-") || len(got) > maxTraceIDLen) {
				t.Fatalf("got %q, want a generated id", got)
			}
		})
	}
}
