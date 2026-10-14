// Package bootstrap starts the service: it loads the configuration,
// initializes logging and dependencies, and runs the HTTP server until a
// shutdown signal arrives.
package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/pprof"
	"os"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/zap"

	"github.com/GoRustC/new-goadmin/internal/config"
	// Registers the generated OpenAPI document with swag.
	_ "github.com/GoRustC/new-goadmin/internal/swagger"
	"github.com/GoRustC/new-goadmin/internal/wirex"
	"github.com/GoRustC/new-goadmin/pkg/logging"
	"github.com/GoRustC/new-goadmin/pkg/util"
)

// RunConfig defines the config for run command.
type RunConfig struct {
	WorkDir   string // Working directory
	Configs   string // Directory or files (multiple separated by commas)
	StaticDir string // Static files directory
}

// Run loads the configuration, initializes logging and the dependencies, and
// serves HTTP until a shutdown signal arrives, then releases everything.
func Run(ctx context.Context, runCfg RunConfig) (err error) {
	defer func() {
		if err := zap.L().Sync(); err != nil {
			fmt.Printf("failed to sync zap logger: %s \n", err.Error())
		}
	}()

	// Load configuration.
	workDir := runCfg.WorkDir
	staticDir := runCfg.StaticDir
	if err := config.Load(workDir, strings.Split(runCfg.Configs, ",")...); err != nil {
		return err
	}
	config.C.General.WorkDir = workDir
	config.C.Middleware.Static.Dir = staticDir
	config.C.Print()
	config.C.PreLoad()
	if err := config.C.Validate(); err != nil {
		return fmt.Errorf("invalid configuration: %w", err)
	}

	// Initialize logger, keeping the hooks so their queues can be monitored.
	var logHooks []*logging.Hook
	cleanLoggerFn, err := logging.InitWithConfig(ctx, &config.C.Logger,
		func(ctx context.Context, cfg *logging.HookConfig) (*logging.Hook, error) {
			h, err := initLoggerHook(ctx, cfg)
			if h != nil {
				logHooks = append(logHooks, h)
			}
			return h, err
		})
	if err != nil {
		return err
	}
	ctx = logging.NewTag(ctx, logging.TagKeyMain)
	// On any startup failure below, log it and flush the log hook before
	// returning; a successful run exits through util.Run's cleanup instead.
	defer func() {
		if err != nil {
			logging.Context(ctx).Error("Failed to start service", zap.Error(err))
			cleanLoggerFn()
		}
	}()

	logging.Context(ctx).Info("starting service ...",
		zap.String("version", config.C.General.Version),
		zap.Int("pid", os.Getpid()),
		zap.String("workdir", workDir),
		zap.String("config", runCfg.Configs),
		zap.String("static", staticDir),
	)

	// Start pprof server on a private mux so its handlers are only reachable
	// on PprofAddr and never leak through http.DefaultServeMux.
	if addr := config.C.General.PprofAddr; addr != "" {
		logging.Context(ctx).Info("pprof server is listening on " + addr)
		go func() {
			mux := http.NewServeMux()
			mux.HandleFunc("/debug/pprof/", pprof.Index)
			mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
			mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
			mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
			mux.HandleFunc("/debug/pprof/trace", pprof.Trace)

			srv := &http.Server{
				Addr:              addr,
				Handler:           mux,
				ReadHeaderTimeout: 10 * time.Second,
			}
			if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				logging.Context(ctx).Error("failed to listen pprof server", zap.Error(err))
			}
		}()
	}

	// Build injector.
	injector, cleanInjectorFn, err := wirex.BuildInjector(ctx)
	if err != nil {
		return err
	}

	if err := injector.M.Init(ctx); err != nil {
		_ = injector.M.Release(ctx)
		cleanInjectorFn()
		return err
	}

	var reg *prometheus.Registry
	if config.C.General.MetricsAddr != "" {
		reg = newMetricsRegistry()
		if err := errors.Join(registerDBMetrics(reg, injector.DB), registerLogHookMetrics(reg, logHooks)); err != nil {
			_ = injector.M.Release(ctx)
			cleanInjectorFn()
			return fmt.Errorf("register metrics: %w", err)
		}
	}

	return util.Run(ctx, func(ctx context.Context) (func(), error) {
		cleanHTTPServerFn, err := startHTTPServer(ctx, injector, reg)
		if err != nil {
			_ = injector.M.Release(ctx)
			cleanInjectorFn()
			return nil, err
		}
		var cleanMetricsServerFn func()
		if reg != nil {
			if cleanMetricsServerFn, err = startMetricsServer(ctx, config.C.General.MetricsAddr, reg); err != nil {
				cleanHTTPServerFn()
				_ = injector.M.Release(ctx)
				cleanInjectorFn()
				return nil, err
			}
		}

		return func() {
			if err := injector.M.Release(ctx); err != nil {
				logging.Context(ctx).Error("failed to release injector", zap.Error(err))
			}

			if cleanHTTPServerFn != nil {
				cleanHTTPServerFn()
			}
			if cleanMetricsServerFn != nil {
				cleanMetricsServerFn()
			}
			if cleanInjectorFn != nil {
				cleanInjectorFn()
			}
			if cleanLoggerFn != nil {
				cleanLoggerFn()
			}
		}, nil
	})
}
