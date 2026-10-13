package bootstrap

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"path/filepath"
	"time"

	"github.com/LyricTian/gin-admin/v10/internal/config"
	"github.com/LyricTian/gin-admin/v10/internal/wirex"
	"github.com/LyricTian/gin-admin/v10/pkg/errors"
	"github.com/LyricTian/gin-admin/v10/pkg/logging"
	"github.com/LyricTian/gin-admin/v10/pkg/middleware"
	"github.com/LyricTian/gin-admin/v10/pkg/util"
	"github.com/casbin/casbin/v2"
	"github.com/gin-gonic/gin"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"
	"go.uber.org/zap"
)

func startHTTPServer(ctx context.Context, injector *wirex.Injector) (func(), error) {
	e, err := NewHTTPHandler(ctx, injector)
	if err != nil {
		return nil, err
	}

	addr := config.C.General.HTTP.Addr
	srv := &http.Server{
		Addr:         addr,
		Handler:      e,
		ReadTimeout:  time.Second * time.Duration(config.C.General.HTTP.ReadTimeout),
		WriteTimeout: time.Second * time.Duration(config.C.General.HTTP.WriteTimeout),
		IdleTimeout:  time.Second * time.Duration(config.C.General.HTTP.IdleTimeout),

		ReadHeaderTimeout: time.Second * time.Duration(config.C.General.HTTP.ReadHeaderTimeout),
		MaxHeaderBytes:    config.C.General.HTTP.MaxHeaderBytes,
	}

	// Load the certificate and bind the port before returning, so a busy port
	// or a bad certificate fails startup instead of leaving a process that is
	// running but not serving.
	certFile, keyFile := config.C.General.HTTP.CertFile, config.C.General.HTTP.KeyFile
	if certFile != "" && keyFile != "" {
		cert, err := tls.LoadX509KeyPair(certFile, keyFile)
		if err != nil {
			return nil, fmt.Errorf("load TLS certificate: %w", err)
		}
		srv.TLSConfig = &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{cert}}
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("listen on %s: %w", addr, err)
	}
	logging.Context(ctx).Info(fmt.Sprintf("HTTP server is listening on %s", ln.Addr()))

	go func() {
		var err error
		if srv.TLSConfig != nil {
			err = srv.ServeTLS(ln, "", "")
		} else {
			err = srv.Serve(ln)
		}
		if err != nil && err != http.ErrServerClosed {
			logging.Context(ctx).Error("HTTP server stopped", zap.Error(err))
		}
	}()

	return func() {
		ctx, cancel := context.WithTimeout(ctx, time.Second*time.Duration(config.C.General.HTTP.ShutdownTimeout))
		defer cancel()

		srv.SetKeepAlivesEnabled(false)
		if err := srv.Shutdown(ctx); err != nil {
			logging.Context(ctx).Error("Failed to shutdown http server", zap.Error(err))
		}
	}, nil
}

// NewHTTPHandler builds the gin engine with every middleware and route the
// server uses, without listening on a port. Tests use it to exercise the
// real request pipeline.
func NewHTTPHandler(ctx context.Context, injector *wirex.Injector) (*gin.Engine, error) {
	if config.C.IsDebug() {
		gin.SetMode(gin.DebugMode)
	} else {
		gin.SetMode(gin.ReleaseMode)
	}

	e := gin.New()
	// gin trusts X-Forwarded-For from every address by default, which lets
	// clients spoof their IP and bypass per-IP rate limiting.
	if err := e.SetTrustedProxies(config.C.General.HTTP.TrustedProxies); err != nil {
		return nil, fmt.Errorf("invalid General.HTTP.TrustedProxies: %w", err)
	}
	e.Use(middleware.SecureHeadersWithConfig(middleware.SecureHeadersConfig{
		APIPathPrefixes: injector.M.RouterPrefixes(),
	}))
	// Liveness: the process is up and serving.
	e.GET("/health", func(c *gin.Context) {
		util.ResOK(c)
	})
	// Readiness: the database and cache are reachable. Load balancers should
	// route traffic only to instances that pass this check.
	e.GET("/health/ready", func(c *gin.Context) {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 3*time.Second)
		defer cancel()
		if err := checkReady(ctx, injector); err != nil {
			logging.Context(ctx).Warn("Readiness check failed", zap.Error(err))
			util.ResJSON(c, http.StatusServiceUnavailable, util.ResponseResult{
				Error: &errors.Error{ID: "not_ready", Code: http.StatusServiceUnavailable, Detail: "Service not ready"},
			})
			return
		}
		util.ResOK(c)
	})
	e.Use(middleware.RecoveryWithConfig(middleware.RecoveryConfig{
		Skip: config.C.Middleware.Recovery.Skip,
	}))
	e.NoMethod(func(c *gin.Context) {
		util.ResError(c, errors.MethodNotAllowed("", "Method Not Allowed"))
	})
	e.NoRoute(func(c *gin.Context) {
		util.ResError(c, errors.NotFound("", "Not Found"))
	})

	allowedPrefixes := injector.M.RouterPrefixes()

	// Register middlewares
	if err := useHTTPMiddlewares(ctx, e, injector, allowedPrefixes); err != nil {
		return nil, err
	}

	// Register routers
	if err := injector.M.RegisterRouters(ctx, e); err != nil {
		return nil, err
	}

	// Register swagger
	if !config.C.General.DisableSwagger {
		e.StaticFile("/openapi.json", filepath.Join(config.C.General.WorkDir, "openapi.json"))
		e.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))
	}

	if dir := config.C.Middleware.Static.Dir; dir != "" {
		e.Use(middleware.StaticWithConfig(middleware.StaticConfig{
			Root:                dir,
			SkippedPathPrefixes: allowedPrefixes,
		}))
	}

	return e, nil
}

func useHTTPMiddlewares(_ context.Context, e *gin.Engine, injector *wirex.Injector, allowedPrefixes []string) error {
	e.Use(middleware.CORSWithConfig(middleware.CORSConfig{
		Enable:                 config.C.Middleware.CORS.Enable,
		AllowAllOrigins:        config.C.Middleware.CORS.AllowAllOrigins,
		AllowOrigins:           config.C.Middleware.CORS.AllowOrigins,
		AllowMethods:           config.C.Middleware.CORS.AllowMethods,
		AllowHeaders:           config.C.Middleware.CORS.AllowHeaders,
		AllowCredentials:       config.C.Middleware.CORS.AllowCredentials,
		ExposeHeaders:          config.C.Middleware.CORS.ExposeHeaders,
		MaxAge:                 config.C.Middleware.CORS.MaxAge,
		AllowWildcard:          config.C.Middleware.CORS.AllowWildcard,
		AllowBrowserExtensions: config.C.Middleware.CORS.AllowBrowserExtensions,
		AllowWebSockets:        config.C.Middleware.CORS.AllowWebSockets,
		AllowFiles:             config.C.Middleware.CORS.AllowFiles,
	}))

	e.Use(middleware.TraceWithConfig(middleware.TraceConfig{
		AllowedPathPrefixes: allowedPrefixes,
		SkippedPathPrefixes: config.C.Middleware.Trace.SkippedPathPrefixes,
		RequestHeaderKey:    config.C.Middleware.Trace.RequestHeaderKey,
		ResponseTraceKey:    config.C.Middleware.Trace.ResponseTraceKey,
	}))

	e.Use(middleware.LoggerWithConfig(middleware.LoggerConfig{
		AllowedPathPrefixes: allowedPrefixes,
		SkippedPathPrefixes: config.C.Middleware.Logger.SkippedPathPrefixes,
	}))

	e.Use(middleware.CopyBodyWithConfig(middleware.CopyBodyConfig{
		AllowedPathPrefixes: allowedPrefixes,
		SkippedPathPrefixes: config.C.Middleware.CopyBody.SkippedPathPrefixes,
		MaxContentLen:       config.C.Middleware.CopyBody.MaxContentLen,
	}))

	e.Use(middleware.AuthWithConfig(middleware.AuthConfig{
		AllowedPathPrefixes: allowedPrefixes,
		SkippedPathPrefixes: config.C.Middleware.Auth.SkippedPathPrefixes,
		ParseUserID:         injector.M.RBAC.LoginAPI.LoginBIZ.ParseUserID,
		RootID:              config.C.General.Root.ID,
	}))

	e.Use(middleware.RateLimiterWithConfig(middleware.RateLimiterConfig{
		Enable:              config.C.Middleware.RateLimiter.Enable,
		AllowedPathPrefixes: allowedPrefixes,
		SkippedPathPrefixes: config.C.Middleware.RateLimiter.SkippedPathPrefixes,
		Period:              config.C.Middleware.RateLimiter.Period,
		MaxRequestsPerIP:    config.C.Middleware.RateLimiter.MaxRequestsPerIP,
		MaxRequestsPerUser:  config.C.Middleware.RateLimiter.MaxRequestsPerUser,
		StoreType:           config.C.Middleware.RateLimiter.Store.Type,
		MemoryStoreConfig: middleware.RateLimiterMemoryConfig{
			Expiration:      time.Second * time.Duration(config.C.Middleware.RateLimiter.Store.Memory.Expiration),
			CleanupInterval: time.Second * time.Duration(config.C.Middleware.RateLimiter.Store.Memory.CleanupInterval),
		},
		RedisStoreConfig: middleware.RateLimiterRedisConfig{
			Addr:     config.C.Middleware.RateLimiter.Store.Redis.Addr,
			Password: config.C.Middleware.RateLimiter.Store.Redis.Password,
			DB:       config.C.Middleware.RateLimiter.Store.Redis.DB,
			Username: config.C.Middleware.RateLimiter.Store.Redis.Username,
		},
	}))

	e.Use(middleware.CasbinWithConfig(middleware.CasbinConfig{
		AllowedPathPrefixes: allowedPrefixes,
		SkippedPathPrefixes: config.C.Middleware.Casbin.SkippedPathPrefixes,
		Skipper: func(c *gin.Context) bool {
			if config.C.Middleware.Casbin.Disable ||
				util.FromIsRootUser(c.Request.Context()) {
				return true
			}
			return false
		},
		GetEnforcer: func(c *gin.Context) *casbin.Enforcer {
			return injector.M.RBAC.Casbinx.GetEnforcer()
		},
		GetSubjects: func(c *gin.Context) []string {
			return util.FromUserCache(c.Request.Context()).RoleIDs
		},
	}))

	return nil
}

// checkReady pings the database and makes a round trip to the cache.
func checkReady(ctx context.Context, injector *wirex.Injector) error {
	sqlDB, err := injector.DB.DB()
	if err != nil {
		return fmt.Errorf("database: %w", err)
	}
	if err := sqlDB.PingContext(ctx); err != nil {
		return fmt.Errorf("database: %w", err)
	}
	if _, err := injector.Cache.Exists(ctx, "health", "ready"); err != nil {
		return fmt.Errorf("cache: %w", err)
	}
	return nil
}
