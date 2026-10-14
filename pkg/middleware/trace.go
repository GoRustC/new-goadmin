package middleware

import (
	"fmt"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/rs/xid"

	"github.com/GoRustC/new-goadmin/pkg/logging"
	"github.com/GoRustC/new-goadmin/pkg/util"
)

// TraceConfig configures TraceWithConfig. A client's ID in RequestHeaderKey
// is reused if valid; the trace ID is returned in ResponseTraceKey.
type TraceConfig struct {
	AllowedPathPrefixes []string
	SkippedPathPrefixes []string
	RequestHeaderKey    string
	ResponseTraceKey    string
}

// DefaultTraceConfig reads X-Request-Id and returns X-Trace-Id.
var DefaultTraceConfig = TraceConfig{
	RequestHeaderKey: "X-Request-Id",
	ResponseTraceKey: "X-Trace-Id",
}

// Trace assigns each request a trace ID with DefaultTraceConfig and stores it
// in the request context for logging.
func Trace() gin.HandlerFunc {
	return TraceWithConfig(DefaultTraceConfig)
}

// TraceWithConfig is Trace with the given configuration.
func TraceWithConfig(config TraceConfig) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !AllowedPathPrefixes(c, config.AllowedPathPrefixes...) ||
			SkippedPathPrefixes(c, config.SkippedPathPrefixes...) {
			c.Next()
			return
		}

		// Accept a caller's request ID only if it is short and plain; otherwise
		// generate one. An oversized ID made the database log insert fail, so
		// a client could keep its requests out of the audit log.
		traceID := c.GetHeader(config.RequestHeaderKey)
		if !validTraceID(traceID) {
			traceID = fmt.Sprintf("TRACE-%s", strings.ToUpper(xid.New().String()))
		}

		ctx := util.NewTraceID(c.Request.Context(), traceID)
		ctx = logging.NewTraceID(ctx, traceID)
		c.Request = c.Request.WithContext(ctx)
		c.Writer.Header().Set(config.ResponseTraceKey, traceID)
		c.Next()
	}
}

// maxTraceIDLen matches the trace_id column of the logger table.
const maxTraceIDLen = 64

func validTraceID(id string) bool {
	if id == "" || len(id) > maxTraceIDLen {
		return false
	}
	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_', r == '.':
		default:
			return false
		}
	}
	return true
}
