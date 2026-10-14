package biz

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	gormschema "gorm.io/gorm/schema"

	"github.com/GoRustC/new-goadmin/internal/config"
	"github.com/GoRustC/new-goadmin/internal/mods/rbac/dal"
	"github.com/GoRustC/new-goadmin/internal/mods/rbac/schema"
)

func TestLoggerRetention(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "log.db")), &gorm.Config{
		NamingStrategy: gormschema.NamingStrategy{SingularTable: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(new(schema.Logger)); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	for _, l := range []schema.Logger{
		{ID: "old", Message: "old", CreatedAt: now.AddDate(0, 0, -10)},
		{ID: "new", Message: "new", CreatedAt: now},
	} {
		if err := db.Create(&l).Error; err != nil {
			t.Fatal(err)
		}
	}

	cfg := new(config.Config)
	cfg.Dictionary.LoggerRetentionDays = 7
	l := &Logger{Config: cfg, LoggerDAL: &dal.Logger{DB: db}}
	l.StartRetention(context.Background())

	deadline := time.Now().Add(5 * time.Second)
	for {
		var ids []string
		db.Model(new(schema.Logger)).Pluck("id", &ids)
		if len(ids) == 1 && ids[0] == "new" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("old entry not deleted, remaining: %v", ids)
		}
		time.Sleep(20 * time.Millisecond)
	}

	done := make(chan struct{})
	go func() { l.StopRetention(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("StopRetention did not return")
	}
	l.StopRetention() // safe to call twice
}
