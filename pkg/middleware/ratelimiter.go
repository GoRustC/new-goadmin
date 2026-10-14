package middleware

import (
	"context"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis_rate/v10"
	"github.com/patrickmn/go-cache"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
	"golang.org/x/time/rate"

	"github.com/GoRustC/new-goadmin/pkg/errors"
	"github.com/GoRustC/new-goadmin/pkg/logging"
	"github.com/GoRustC/new-goadmin/pkg/util"
)

type RateLimiterConfig struct {
	Enable              bool
	AllowedPathPrefixes []string
	SkippedPathPrefixes []string
	Period              int
	MaxRequestsPerIP    int
	MaxRequestsPerUser  int
	StoreType           string // memory/redis
	MemoryStoreConfig   RateLimiterMemoryConfig
	RedisStoreConfig    RateLimiterRedisConfig
}

func RateLimiterWithConfig(config RateLimiterConfig) gin.HandlerFunc {
	if !config.Enable {
		return Empty()
	}

	var store RateLimiterStorer
	switch config.StoreType {
	case "redis":
		store = NewRateLimiterRedisStore(config.RedisStoreConfig)
	default:
		store = NewRateLimiterMemoryStore(config.MemoryStoreConfig)
	}

	return func(c *gin.Context) {
		if !AllowedPathPrefixes(c, config.AllowedPathPrefixes...) ||
			SkippedPathPrefixes(c, config.SkippedPathPrefixes...) {
			c.Next()
			return
		}

		var (
			allowed bool
			err     error
		)

		ctx := c.Request.Context()
		if userID := util.FromUserID(ctx); userID != "" {
			allowed, err = store.Allow(ctx, "user:"+userID, time.Second*time.Duration(config.Period), config.MaxRequestsPerUser)
		} else {
			allowed, err = store.Allow(ctx, "ip:"+c.ClientIP(), time.Second*time.Duration(config.Period), config.MaxRequestsPerIP)
		}

		if err != nil {
			logging.Context(ctx).Error("Rate limiter middleware error", zap.Error(err))
			util.ResError(c, errors.InternalServerError("", "Internal server error, please try again later."))
		} else if allowed {
			c.Next()
		} else {
			util.ResError(c, errors.TooManyRequests("", "Too many requests, please try again later."))
		}
	}
}

type RateLimiterStorer interface {
	Allow(ctx context.Context, identifier string, period time.Duration, maxRequests int) (bool, error)
}

func NewRateLimiterMemoryStore(config RateLimiterMemoryConfig) RateLimiterStorer {
	return &RateLimiterMemoryStore{
		cache: cache.New(config.Expiration, config.CleanupInterval),
	}
}

type RateLimiterMemoryConfig struct {
	Expiration      time.Duration
	CleanupInterval time.Duration
}

type RateLimiterMemoryStore struct {
	cache *cache.Cache
}

func (s *RateLimiterMemoryStore) Allow(ctx context.Context, identifier string, period time.Duration, maxRequests int) (bool, error) {
	if period.Seconds() <= 0 || maxRequests <= 0 {
		return true, nil
	}

	if limiter, exists := s.cache.Get(identifier); exists {
		isAllow := limiter.(*rate.Limiter).Allow()
		s.cache.SetDefault(identifier, limiter)
		return isAllow, nil
	}

	// Refill maxRequests tokens per period, allowing a burst of maxRequests.
	limiter := rate.NewLimiter(rate.Limit(float64(maxRequests)/period.Seconds()), maxRequests)
	limiter.Allow()
	s.cache.SetDefault(identifier, limiter)

	return true, nil
}

type RateLimiterRedisConfig struct {
	Addr     string
	Username string
	Password string
	DB       int
	// KeyPrefix is prepended to every identifier, see cachex.WithKeyPrefix.
	// redis_rate adds its own fixed "rate:" in front, so keys look like
	// "rate:<KeyPrefix>ip:<addr>".
	KeyPrefix string
}

func NewRateLimiterRedisStore(config RateLimiterRedisConfig) RateLimiterStorer {
	rdb := redis.NewClient(&redis.Options{
		Addr:     config.Addr,
		Username: config.Username,
		Password: config.Password,
		DB:       config.DB,
	})

	return &RateLimiterRedisStore{
		limiter: redis_rate.NewLimiter(rdb),
		prefix:  config.KeyPrefix,
	}
}

type RateLimiterRedisStore struct {
	limiter *redis_rate.Limiter
	prefix  string
}

func (s *RateLimiterRedisStore) Allow(ctx context.Context, identifier string, period time.Duration, maxRequests int) (bool, error) {
	if period.Seconds() <= 0 || maxRequests <= 0 {
		return true, nil
	}

	result, err := s.limiter.Allow(ctx, s.prefix+identifier, redis_rate.Limit{
		Rate:   maxRequests,
		Burst:  maxRequests,
		Period: period,
	})
	if err != nil {
		return false, err
	}
	return result.Allowed > 0, nil
}
