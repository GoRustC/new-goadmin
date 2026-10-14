package middleware

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus"
)

// unmatchedRoute labels requests that matched no route, so scanners probing
// random paths cannot create unbounded label values.
const unmatchedRoute = "unmatched"

// Metrics returns a middleware that records, for every request, the count
// and latency by method, route template (such as /api/v1/users/:id) and
// status, and the number of requests in flight. The metrics are registered
// with reg and named with the given namespace.
func Metrics(reg prometheus.Registerer, namespace string) gin.HandlerFunc {
	requests := prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "http_requests_total",
		Help:      "HTTP requests by method, route and status code.",
	}, []string{"method", "route", "code"})
	duration := prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: namespace,
		Name:      "http_request_duration_seconds",
		Help:      "HTTP request latency by method and route.",
		Buckets:   []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10},
	}, []string{"method", "route"})
	inFlight := prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "http_requests_in_flight",
		Help:      "HTTP requests being served.",
	})
	reg.MustRegister(requests, duration, inFlight)

	return func(c *gin.Context) {
		start := time.Now()
		inFlight.Inc()
		defer inFlight.Dec()

		c.Next()

		route := c.FullPath()
		if route == "" {
			route = unmatchedRoute
		}
		method := metricMethod(c.Request.Method)
		requests.WithLabelValues(method, route, strconv.Itoa(c.Writer.Status())).Inc()
		duration.WithLabelValues(method, route).Observe(time.Since(start).Seconds())
	}
}

// metricMethod returns m if it is a standard HTTP method and "other"
// otherwise, since clients can send any token as the method.
func metricMethod(m string) string {
	switch m {
	case http.MethodGet, http.MethodHead, http.MethodPost, http.MethodPut, http.MethodPatch,
		http.MethodDelete, http.MethodOptions, http.MethodConnect, http.MethodTrace:
		return m
	}
	return "other"
}
