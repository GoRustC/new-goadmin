package dal

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/GoRustC/new-goadmin/internal/mods/rbac/schema"
	"github.com/GoRustC/new-goadmin/pkg/util"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	gormschema "gorm.io/gorm/schema"
)

func TestLoggerQueryFiltersAndFillsUserNames(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "x.db")), &gorm.Config{
		NamingStrategy: gormschema.NamingStrategy{SingularTable: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(new(schema.User), new(schema.Logger)); err != nil {
		t.Fatal(err)
	}
	db.Create(&schema.User{ID: "u1", Username: "Alice", Name: "Alice A"})
	db.Create(&schema.User{ID: "u2", Username: "bob", Name: "Bob B"})
	now := time.Now()
	for i, uid := range []string{"u1", "u1", "u2", ""} {
		db.Create(&schema.Logger{ID: string(rune('a' + i)), UserID: uid, Level: "info", Message: "m", CreatedAt: now.Add(time.Duration(i) * time.Second)})
	}

	l := &Logger{DB: db}
	ctx := context.Background()
	page := util.PaginationParam{Pagination: true, Current: 1, PageSize: 10}

	all, err := l.Query(ctx, schema.LoggerQueryParam{PaginationParam: page})
	if err != nil {
		t.Fatal(err)
	}
	if all.PageResult.Total != 4 || len(all.Data) != 4 {
		t.Fatalf("total=%d rows=%d, want 4 and 4", all.PageResult.Total, len(all.Data))
	}
	names := map[string]string{}
	for _, row := range all.Data {
		names[row.ID] = row.LoginName + "/" + row.UserName
	}
	if names["a"] != "Alice/Alice A" || names["c"] != "bob/Bob B" || names["d"] != "/" {
		t.Fatalf("user names not filled: %v", names)
	}

	alice, err := l.Query(ctx, schema.LoggerQueryParam{PaginationParam: page, LikeUserName: "alice"})
	if err != nil {
		t.Fatal(err)
	}
	if alice.PageResult.Total != 2 || len(alice.Data) != 2 {
		t.Fatalf("filter by user name: total=%d rows=%d, want 2", alice.PageResult.Total, len(alice.Data))
	}
}
