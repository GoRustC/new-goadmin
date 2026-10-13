package middleware

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func gzipped(b []byte) []byte {
	var buf bytes.Buffer
	w := gzip.NewWriter(&buf)
	_, _ = w.Write(b)
	_ = w.Close()
	return buf.Bytes()
}

func TestCopyBody(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var got []byte
	e := gin.New()
	e.Use(CopyBodyWithConfig(CopyBodyConfig{MaxContentLen: 1024}))
	e.POST("/x", func(c *gin.Context) {
		got, _ = io.ReadAll(c.Request.Body)
		c.Status(http.StatusOK)
	})
	send := func(body []byte, gz bool) int {
		got = nil
		req := httptest.NewRequest(http.MethodPost, "/x", bytes.NewReader(body))
		if gz {
			req.Header.Set("Content-Encoding", "gzip")
		}
		w := httptest.NewRecorder()
		e.ServeHTTP(w, req)
		return w.Code
	}

	if code := send([]byte(`{"a":1}`), false); code != 200 || string(got) != `{"a":1}` {
		t.Fatalf("plain body: %d %q", code, got)
	}
	if code := send(gzipped([]byte(`{"a":1}`)), true); code != 200 || string(got) != `{"a":1}` {
		t.Fatalf("gzip body should reach the handler decompressed: %d %q", code, got)
	}
	// Not gzip at all: used to fall back to reading the rest of the body,
	// handing the handler data with its first bytes missing.
	if code := send([]byte(`{"a":1, "padding": "xxxxxxxxxxxx"}`), true); code != http.StatusBadRequest || got != nil {
		t.Fatalf("invalid gzip header: %d, handler got %q", code, got)
	}
	// Corrupt gzip data is a bad request, not "too large".
	corrupt := gzipped(bytes.Repeat([]byte("a"), 100))
	corrupt[len(corrupt)-5] ^= 0xff
	if code := send(corrupt, true); code != http.StatusBadRequest {
		t.Fatalf("corrupt gzip: %d, want 400", code)
	}
	if code := send(bytes.Repeat([]byte("a"), 2048), false); code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized body: %d, want 413", code)
	}
	if code := send(gzipped(bytes.Repeat([]byte("a"), 4096)), true); code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized after decompression: %d, want 413", code)
	}
}
