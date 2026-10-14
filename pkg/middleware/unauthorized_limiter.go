package middleware

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/GoRustC/new-goadmin/pkg/errors"
	"github.com/GoRustC/new-goadmin/pkg/logging"
	"github.com/GoRustC/new-goadmin/pkg/util"
)

// FailureCounter stores per-key counters; cachex.Cacher satisfies it.
type FailureCounter interface {
	Get(ctx context.Context, ns, key string) (string, bool, error)
	Incr(ctx context.Context, ns, key string, expiration time.Duration) (int64, error)
}

// UnauthorizedLimiterConfig configures UnauthorizedLimiterWithConfig. A
// client IP may receive MaxFailures 401 responses per Period before its
// requests are refused with 429.
type UnauthorizedLimiterConfig struct {
	Enable              bool
	AllowedPathPrefixes []string
	SkippedPathPrefixes []string
	Period              time.Duration
	MaxFailures         int
	Counter             FailureCounter
}

// unauthorizedNS is the counter namespace in the cache.
const unauthorizedNS = "unauthorized"

// UnauthorizedLimiterWithConfig returns a middleware that limits requests
// rejected as unauthenticated, per client IP.
//
// The per-IP rate limiter runs after authentication, so it never sees
// requests with a missing, invalid or revoked token: they are answered 401
// first. Without this limit, one client could send such requests without
// bound, and each would be written to the operation log, filling its queue
// until real entries are dropped. Install this before the logger so refused
// requests are not logged; they are still counted by the metrics.
//
// Authenticated requests are never counted. If the counter store fails, the
// request proceeds: authentication still applies.
func UnauthorizedLimiterWithConfig(config UnauthorizedLimiterConfig) gin.HandlerFunc {
	if !config.Enable || config.MaxFailures <= 0 || config.Period <= 0 {
		return Empty()
	}

	return func(c *gin.Context) {
		if !AllowedPathPrefixes(c, config.AllowedPathPrefixes...) ||
			SkippedPathPrefixes(c, config.SkippedPathPrefixes...) {
			c.Next()
			return
		}

		ctx := c.Request.Context()
		ip := c.ClientIP()
		if v, ok, err := config.Counter.Get(ctx, unauthorizedNS, ip); err != nil {
			logging.Context(ctx).Error("Failed to read the unauthorized request count", zap.Error(err))
		} else if ok {
			if n, _ := strconv.Atoi(v); n >= config.MaxFailures {
				util.ResError(c, errors.TooManyRequests("", "Too many unauthenticated requests, please try again later."))
				return
			}
		}

		c.Next()

		if c.Writer.Status() == http.StatusUnauthorized {
			if _, err := config.Counter.Incr(ctx, unauthorizedNS, ip, config.Period); err != nil {
				logging.Context(ctx).Error("Failed to count an unauthorized request", zap.Error(err))
			}
		}
	}
}
