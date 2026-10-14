package logging

import (
	"fmt"
	"time"
	"unicode/utf8"

	"encoding/json"

	"github.com/rs/xid"
	"gorm.io/gorm"
)

type Logger struct {
	ID        string    `gorm:"size:20;primaryKey;" json:"id"`  // Unique ID
	Level     string    `gorm:"size:20;index;" json:"level"`    // Log level
	TraceID   string    `gorm:"size:64;index;" json:"trace_id"` // Trace ID
	UserID    string    `gorm:"size:20;index;" json:"user_id"`  // User ID
	Tag       string    `gorm:"size:32;index;" json:"tag"`      // Log tag
	Message   string    `gorm:"size:1024;" json:"message"`      // Log message
	Stack     string    `gorm:"type:text;" json:"stack"`        // Error stack
	Data      string    `gorm:"type:text;" json:"data"`         // Log data
	CreatedAt time.Time `gorm:"index;" json:"created_at"`       // Create time
}

func NewGormHook(db *gorm.DB) *GormHook {
	err := db.AutoMigrate(new(Logger))
	if err != nil {
		panic(err)
	}

	return &GormHook{
		db: db,
	}
}

// Gorm Logger Hook
type GormHook struct {
	db *gorm.DB
}

func (h *GormHook) Exec(extra map[string]string, b []byte) error {
	msg, err := toLogger(extra, b)
	if err != nil {
		return err
	}
	return h.db.Create(msg).Error
}

// ExecBatch writes several entries with one INSERT. Entries that are not
// valid JSON are skipped so they cannot drop the rest of the batch.
func (h *GormHook) ExecBatch(extra map[string]string, entries [][]byte) error {
	rows := make([]*Logger, 0, len(entries))
	for _, b := range entries {
		msg, err := toLogger(extra, b)
		if err != nil {
			fmt.Println("Skipping malformed log entry:", err.Error())
			continue
		}
		rows = append(rows, msg)
	}
	if len(rows) == 0 {
		return nil
	}
	return h.db.CreateInBatches(rows, len(rows)).Error
}

// toLogger maps a zap JSON entry to a logger table row.
func toLogger(extra map[string]string, b []byte) (*Logger, error) {
	msg := &Logger{
		ID: xid.New().String(),
	}
	data := make(map[string]any)
	if err := json.Unmarshal(b, &data); err != nil {
		return nil, err
	}

	if v, ok := data["ts"].(float64); ok {
		msg.CreatedAt = time.UnixMilli(int64(v))
	}
	delete(data, "ts")
	// Values are truncated to the column sizes: MySQL in strict mode and
	// PostgreSQL reject longer values, which would drop the whole entry.
	msg.Message = takeString(data, "msg", 1024)
	msg.Tag = takeString(data, "tag", 32)
	msg.TraceID = takeString(data, "trace_id", 64)
	msg.UserID = takeString(data, "user_id", 20)
	msg.Level = takeString(data, "level", 20)
	msg.Stack = takeString(data, "stack", 0)
	delete(data, "caller")

	for k, v := range extra {
		data[k] = v
	}

	if len(data) > 0 {
		buf, _ := json.Marshal(data)
		msg.Data = string(buf)
	}
	return msg, nil
}

func (h *GormHook) Close() error {
	db, err := h.db.DB()
	if err != nil {
		return err
	}
	return db.Close()
}

// takeString removes key from data and returns it as a string of at most max
// characters (0 means no limit). Non-string values become "".
func takeString(data map[string]any, key string, max int) string {
	v, _ := data[key].(string)
	delete(data, key)
	if max > 0 && utf8.RuneCountInString(v) > max {
		v = string([]rune(v)[:max])
	}
	return v
}
