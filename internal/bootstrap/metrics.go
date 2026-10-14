package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.uber.org/zap"
	"gorm.io/gorm"

	"github.com/GoRustC/new-goadmin/pkg/logging"
)

// metricsNamespace prefixes the application's own metrics.
const metricsNamespace = "ginadmin"

// newMetricsRegistry returns a registry with the Go runtime and process
// collectors. A private registry, rather than the global default, exports
// only what this service registers.
func newMetricsRegistry() *prometheus.Registry {
	reg := prometheus.NewRegistry()
	reg.MustRegister(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
	)
	return reg
}

// registerDBMetrics exports the connection pool statistics of db, such as
// open and in-use connections and time spent waiting for one.
func registerDBMetrics(reg prometheus.Registerer, db *gorm.DB) error {
	sqlDB, err := db.DB()
	if err != nil {
		return err
	}
	return reg.Register(collectors.NewDBStatsCollector(sqlDB, "main"))
}

// registerLogHookMetrics exports the queue of each log hook. Dropped entries
// mean the operation log is missing records.
func registerLogHookMetrics(reg prometheus.Registerer, hooks []*logging.Hook) error {
	for i, h := range hooks {
		labels := prometheus.Labels{"hook": fmt.Sprint(i)}
		for _, c := range []prometheus.Collector{
			prometheus.NewGaugeFunc(prometheus.GaugeOpts{
				Namespace: metricsNamespace, Name: "log_hook_queued_entries",
				Help: "Log entries waiting to be written by the hook.", ConstLabels: labels,
			}, func() float64 { return float64(h.Queued()) }),
			prometheus.NewCounterFunc(prometheus.CounterOpts{
				Namespace: metricsNamespace, Name: "log_hook_dropped_entries_total",
				Help: "Log entries discarded because the hook's queue was full.", ConstLabels: labels,
			}, func() float64 { return float64(h.Dropped()) }),
			prometheus.NewCounterFunc(prometheus.CounterOpts{
				Namespace: metricsNamespace, Name: "log_hook_failed_entries_total",
				Help: "Log entries the hook failed to write.", ConstLabels: labels,
			}, func() float64 { return float64(h.Failed()) }),
		} {
			if err := reg.Register(c); err != nil {
				return err
			}
		}
	}
	return nil
}

// startMetricsServer serves the metrics of reg at /metrics on addr. The port
// is bound before it returns, so a busy port fails startup. The returned
// function shuts the server down.
func startMetricsServer(ctx context.Context, addr string, reg *prometheus.Registry) (func(), error) {
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.HandlerFor(reg, promhttp.HandlerOpts{Registry: reg}))
	srv := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("listen for metrics on %s: %w", addr, err)
	}
	logging.Context(ctx).Info(fmt.Sprintf("Metrics server is listening on %s", ln.Addr()))
	go func() {
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logging.Context(ctx).Error("Metrics server stopped", zap.Error(err))
		}
	}()

	return func() {
		ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		if err := srv.Shutdown(ctx); err != nil {
			logging.Context(ctx).Error("Failed to shut down the metrics server", zap.Error(err))
		}
	}, nil
}
