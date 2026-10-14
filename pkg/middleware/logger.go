package middleware

import (
	"fmt"
	"net/url"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/GoRustC/new-goadmin/pkg/logging"
)

type LoggerConfig struct {
	AllowedPathPrefixes []string
	SkippedPathPrefixes []string
}

var DefaultLoggerConfig = LoggerConfig{}

// Record request metadata for quick troubleshooting.
// Request and response bodies are intentionally not logged: they may carry
// credentials (login passwords, access tokens) that must never reach log storage.
func Logger() gin.HandlerFunc {
	return LoggerWithConfig(DefaultLoggerConfig)
}

func LoggerWithConfig(config LoggerConfig) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !AllowedPathPrefixes(c, config.AllowedPathPrefixes...) ||
			SkippedPathPrefixes(c, config.SkippedPathPrefixes...) {
			c.Next()
			return
		}

		start := time.Now()
		contentType := c.Request.Header.Get("Content-Type")

		fields := []zap.Field{
			zap.String("client_ip", c.ClientIP()),
			zap.String("method", c.Request.Method),
			zap.String("path", c.Request.URL.Path),
			zap.String("user_agent", c.Request.UserAgent()),
			zap.String("referer", c.Request.Referer()),
			zap.String("uri", redactedURI(c.Request.URL)),
			zap.String("host", c.Request.Host),
			zap.String("remote_addr", c.Request.RemoteAddr),
			zap.String("proto", c.Request.Proto),
			zap.Int64("content_length", c.Request.ContentLength),
			zap.String("content_type", contentType),
			zap.String("pragma", c.Request.Header.Get("Pragma")),
		}

		c.Next()

		cost := time.Since(start).Nanoseconds() / 1e6
		fields = append(fields, zap.Int64("cost", cost))
		fields = append(fields, zap.Int("status", c.Writer.Status()))
		fields = append(fields, zap.String("res_time", time.Now().Format("2006-01-02 15:04:05.999")))
		fields = append(fields, zap.Int("res_size", c.Writer.Size()))

		ctx := c.Request.Context()
		ctx = logging.NewTag(ctx, logging.TagKeyRequest)
		logging.Context(ctx).Info(fmt.Sprintf("[HTTP] %s-%s-%d (%dms)",
			c.Request.URL.Path, c.Request.Method, c.Writer.Status(), cost), fields...)
	}
}

// Query parameters that carry credentials and must not reach the logs.
// accessToken is accepted by util.GetToken as an alternative to the
// Authorization header.
var sensitiveQueryParams = []string{"accessToken", "access_token", "token"}

// redactedURI returns the request path and query with credential values
// masked.
func redactedURI(u *url.URL) string {
	if u.RawQuery == "" {
		return u.Path
	}
	q := u.Query()
	for _, k := range sensitiveQueryParams {
		if q.Has(k) {
			q.Set(k, "******")
		}
	}
	return u.Path + "?" + q.Encode()
}
