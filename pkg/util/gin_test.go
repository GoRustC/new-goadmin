package util

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/LyricTian/gin-admin/v10/pkg/errors"
	"github.com/gin-gonic/gin"
)

// ResError used to modify the error it was given. Shared error values, such
// as the casbin "permission denied" error, were mutated by concurrent
// requests and could be rewritten for every later response.
func TestResErrorDoesNotModifySharedError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	shared := errors.Forbidden("shared", "Permission denied").(*errors.Error)

	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(http.MethodGet, "/", nil)
			if i%2 == 0 {
				ResError(c, shared, http.StatusTeapot)
			} else {
				ResError(c, shared)
			}
		}(i)
	}
	wg.Wait()

	if shared.Code != http.StatusForbidden || shared.Detail != "Permission denied" {
		t.Fatalf("shared error was modified: %+v", shared)
	}
}
