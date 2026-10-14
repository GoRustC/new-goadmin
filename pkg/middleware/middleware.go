package middleware

import (
	"strings"

	"github.com/gin-gonic/gin"
)

// matchPathPrefix reports whether path falls under prefix, matching whole
// path segments. A prefix ending in "/" covers everything below it
// ("/api/v1/captcha/" matches "/api/v1/captcha/id"). Any other prefix covers
// the path itself and its sub-paths only: "/api/v1/login" matches
// "/api/v1/login" and "/api/v1/login/x" but not "/api/v1/login-history",
// which a raw string prefix would have exempted from authentication.
func matchPathPrefix(path, prefix string) bool {
	if strings.HasSuffix(prefix, "/") {
		return strings.HasPrefix(path, prefix)
	}
	return path == prefix || strings.HasPrefix(path, prefix+"/")
}

func matchAnyPathPrefix(path string, prefixes []string) bool {
	for _, p := range prefixes {
		if matchPathPrefix(path, p) {
			return true
		}
	}
	return false
}

// SkippedPathPrefixes reports whether the request path is under any of the
// prefixes. No prefixes means nothing is skipped.
func SkippedPathPrefixes(c *gin.Context, prefixes ...string) bool {
	return len(prefixes) > 0 && matchAnyPathPrefix(c.Request.URL.Path, prefixes)
}

// AllowedPathPrefixes reports whether the request path is under any of the
// prefixes. No prefixes means everything is allowed.
func AllowedPathPrefixes(c *gin.Context, prefixes ...string) bool {
	return len(prefixes) == 0 || matchAnyPathPrefix(c.Request.URL.Path, prefixes)
}

// Empty returns a middleware that does nothing.
func Empty() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Next()
	}
}
