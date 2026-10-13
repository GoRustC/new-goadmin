package logging

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.uber.org/zap"
)

// The caller field must point at the line that logged, not one frame up.
func TestCallerPointsAtTheLoggingLine(t *testing.T) {
	file := filepath.Join(t.TempDir(), "app.log")
	cfg := &LoggerConfig{Level: "info", CallerSkip: 0}
	cfg.File.Enable = true
	cfg.File.Path = file
	cfg.File.MaxSize = 1

	orig := zap.L()
	t.Cleanup(func() { zap.ReplaceGlobals(orig) })
	cleanup, err := InitWithConfig(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	Context(context.Background()).Info("hello") // the line the caller field must name
	cleanup()

	b, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	var entry struct {
		Caller string `json:"caller"`
	}
	if err := json.Unmarshal([]byte(strings.SplitN(string(b), "\n", 2)[0]), &entry); err != nil {
		t.Fatalf("decode %q: %v", b, err)
	}
	if !strings.HasPrefix(entry.Caller, "logging/init_test.go:") {
		t.Fatalf("caller = %q, want logging/init_test.go:<line>", entry.Caller)
	}
}
