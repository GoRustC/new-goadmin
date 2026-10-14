package util

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/GoRustC/new-goadmin/pkg/errors"
)

// ResError used to modify the error it was given. Shared error values, such
// as the casbin "permission denied" error, were mutated by concurrent
// requests and could be rewritten for every later response.
func TestResErrorDoesNotModifySharedError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	shared, _ := errors.AsError(errors.Forbidden("shared", "Permission denied"))

	var wg sync.WaitGroup
	for i := range 32 {
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

func TestResJSONEncodeFailure(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/x", nil)

	ResJSON(c, http.StatusOK, map[string]any{"bad": make(chan int)}) // channels cannot be encoded

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status %d, want 500", w.Code)
	}
	var body ResponseResult
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil || body.Success || body.Error == nil {
		t.Fatalf("expected a JSON error envelope, got %q (%v)", w.Body.String(), err)
	}
}

func TestResPageEmptyValues(t *testing.T) {
	gin.SetMode(gin.TestMode)
	type item struct{ ID int }
	var nilSlice []item
	for name, v := range map[string]any{
		"nil":                nil,
		"nil slice":          nilSlice,
		"pointer to nil":     &nilSlice,
		"struct, not a list": item{ID: 1},
	} {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodGet, "/x", nil)
		ResPage(c, v, &PaginationResult{Total: 0}) // used to panic for nil and non-list values
		if w.Code != http.StatusOK {
			t.Errorf("%s: status %d", name, w.Code)
		}
	}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/x", nil)
	ResPage(c, nilSlice, nil)
	if !strings.Contains(w.Body.String(), `"data":[]`) {
		t.Fatalf("an empty list should render as [], got %s", w.Body.String())
	}
}
