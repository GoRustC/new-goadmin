package middleware

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestStaticStaysInsideRoot(t *testing.T) {
	gin.SetMode(gin.TestMode)
	base := t.TempDir()
	root := filepath.Join(base, "www")
	if err := os.Mkdir(root, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "index.html"), []byte("index"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "app.js"), []byte("app"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(base, "secret.txt"), []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}

	e := gin.New()
	e.Use(StaticWithConfig(StaticConfig{Root: root}))

	for path, want := range map[string]string{
		"/app.js":        "app",
		"/missing/route": "index",
	} {
		if got := serveStatic(e, path); got != want {
			t.Errorf("GET %s = %q, want %q", path, got, want)
		}
	}
	// net/http refuses paths containing "..", so nothing outside Root is
	// reachable even though the middleware joins the raw path.
	for _, path := range []string{"/../secret.txt", "/a/../../secret.txt"} {
		if got := serveStatic(e, path); got == "secret" {
			t.Errorf("GET %s served a file outside the root", path)
		}
	}
}

func serveStatic(e *gin.Engine, path string) string {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.URL.Path = path // as sent on the wire, without client-side cleaning
	w := httptest.NewRecorder()
	e.ServeHTTP(w, req)
	return w.Body.String()
}
