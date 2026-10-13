package middleware

import (
	"github.com/gin-gonic/gin"
)

type SecureHeadersConfig struct {
	// API path prefixes that only return data. They also get a
	// Content-Security-Policy that forbids loading or framing anything.
	APIPathPrefixes []string
}

// SecureHeadersWithConfig sets defensive response headers. HSTS is only sent
// when this process terminates TLS; behind a proxy, set it at the proxy.
func SecureHeadersWithConfig(config SecureHeadersConfig) gin.HandlerFunc {
	return func(c *gin.Context) {
		h := c.Writer.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		if c.Request.TLS != nil {
			h.Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		}
		if AllowedPathPrefixes(c, config.APIPathPrefixes...) && len(config.APIPathPrefixes) > 0 {
			h.Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'")
		}
		c.Next()
	}
}
