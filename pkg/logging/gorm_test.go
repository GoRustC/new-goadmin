package logging

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func TestGormHookTruncatesAndToleratesTypes(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	h, err := NewGormHook(db)
	if err != nil {
		t.Fatal(err)
	}

	entry := `{"ts":1700000000000,"level":"info","msg":"` + strings.Repeat("é", 2000) +
		`","trace_id":"` + strings.Repeat("t", 200) + `","user_id":12345,"tag":"login","extra":"kept"}`
	if err := h.Exec(map[string]string{"appname": "test"}, []byte(entry)); err != nil {
		t.Fatal(err)
	}

	var got Logger
	if err := db.First(&got).Error; err != nil {
		t.Fatal(err)
	}
	if n := utf8.RuneCountInString(got.Message); n != 1024 {
		t.Errorf("message has %d characters, want 1024", n)
	}
	if len(got.TraceID) != 64 {
		t.Errorf("trace id has %d characters, want 64", len(got.TraceID))
	}
	if got.UserID != "" {
		t.Errorf("non-string user_id should be dropped, got %q", got.UserID)
	}
	if got.Tag != "login" || got.Level != "info" || got.CreatedAt.IsZero() {
		t.Errorf("fields not mapped: %+v", got)
	}
	if !strings.Contains(got.Data, `"extra":"kept"`) || !strings.Contains(got.Data, `"appname":"test"`) {
		t.Errorf("remaining fields should go to data: %q", got.Data)
	}
}

func TestGormHookExecBatch(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	h, err := NewGormHook(db)
	if err != nil {
		t.Fatal(err)
	}
	entries := [][]byte{
		[]byte(`{"level":"info","msg":"one"}`),
		[]byte(`not json`),
		[]byte(`{"level":"warn","msg":"two"}`),
	}
	if err := h.ExecBatch(nil, entries); err != nil {
		t.Fatal(err)
	}
	var n int64
	db.Model(new(Logger)).Count(&n)
	if n != 2 {
		t.Fatalf("stored %d rows, want 2 (the malformed entry is skipped)", n)
	}
}
