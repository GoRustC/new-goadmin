package e2e

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus/promhttp"
)

func TestRequestMetrics(t *testing.T) {
	root := mustLogin(t, "admin", rootPassword)
	expectStatus(t, "user list", call(t, http.MethodGet, "/api/v1/users?current=1&pageSize=1", root, nil), http.StatusOK)
	expectStatus(t, "unknown user", call(t, http.MethodGet, "/api/v1/users/no-such-user", root, nil), http.StatusNotFound)
	expectStatus(t, "unknown path", call(t, http.MethodGet, "/no/such/path/x1", root, nil), http.StatusNotFound)
	expectStatus(t, "unauthenticated", call(t, http.MethodGet, "/api/v1/users", "", nil), http.StatusUnauthorized)

	w := httptest.NewRecorder()
	promhttp.HandlerFor(metricsRegistry, promhttp.HandlerOpts{}).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	b, _ := io.ReadAll(w.Body)
	body := string(b)

	for _, want := range []string{
		`ginadmin_http_requests_total{code="200",method="GET",route="/api/v1/users"}`,
		// Path parameters are reported as the route template, not the value.
		`ginadmin_http_requests_total{code="404",method="GET",route="/api/v1/users/:id"}`,
		`ginadmin_http_requests_total{code="404",method="GET",route="unmatched"}`,
		`ginadmin_http_requests_total{code="401",method="GET",route="/api/v1/users"}`,
		`ginadmin_http_request_duration_seconds_bucket{method="GET",route="/api/v1/users",le="0.1"}`,
		`ginadmin_http_requests_in_flight`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("metrics lack %s", want)
		}
	}
	for _, leaked := range []string{"no-such-user", "/no/such/path"} {
		if strings.Contains(body, leaked) {
			t.Errorf("metrics contain the raw path %q", leaked)
		}
	}
}
