package biz

import (
	"context"
	"path/filepath"
	"sort"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	gormschema "gorm.io/gorm/schema"

	"github.com/GoRustC/new-goadmin/internal/mods/rbac/dal"
	"github.com/GoRustC/new-goadmin/internal/mods/rbac/schema"
)

func TestMenuQueryAddsRelativesAndResources(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "menu.db")), &gorm.Config{
		NamingStrategy: gormschema.NamingStrategy{SingularTable: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(new(schema.Menu), new(schema.MenuResource)); err != nil {
		t.Fatal(err)
	}

	// a ─┬─ b ── c
	//    └─ s
	// d
	for _, m := range []schema.Menu{
		{ID: "a", Code: "a", Name: "Root A"},
		{ID: "b", Code: "b", Name: "Target", ParentID: "a", ParentPath: "a."},
		{ID: "c", Code: "c", Name: "Leaf C", ParentID: "b", ParentPath: "a.b."},
		{ID: "s", Code: "s", Name: "Sibling", ParentID: "a", ParentPath: "a."},
		{ID: "d", Code: "d", Name: "Other root"},
	} {
		if err := db.Create(&m).Error; err != nil {
			t.Fatal(err)
		}
	}
	for _, r := range []schema.MenuResource{
		{ID: "r1", MenuID: "a", Method: "GET", Path: "/a"},
		{ID: "r2", MenuID: "c", Method: "GET", Path: "/c"},
		{ID: "r3", MenuID: "c", Method: "POST", Path: "/c"},
		{ID: "r4", MenuID: "d", Method: "GET", Path: "/d"},
	} {
		if err := db.Create(&r).Error; err != nil {
			t.Fatal(err)
		}
	}

	m := &Menu{MenuDAL: &dal.Menu{DB: db}, MenuResourceDAL: &dal.MenuResource{DB: db}}
	result, err := m.Query(context.Background(), schema.MenuQueryParam{LikeName: "Target", IncludeResources: true})
	if err != nil {
		t.Fatal(err)
	}

	// The result is a tree; collect every node.
	got := map[string]int{}
	var ids []string
	var walk func(schema.Menus)
	walk = func(items schema.Menus) {
		for _, item := range items {
			if item.Resources == nil {
				t.Errorf("menu %s: resources is nil, want an empty list", item.ID)
			}
			got[item.ID] = len(item.Resources)
			ids = append(ids, item.ID)
			if item.Children != nil {
				walk(*item.Children)
			}
		}
	}
	walk(result.Data)
	sort.Strings(ids)
	if want := []string{"a", "b", "c"}; len(ids) != len(want) || ids[0] != want[0] || ids[1] != want[1] || ids[2] != want[2] {
		t.Fatalf("menus = %v, want the match with its ancestor and descendant %v", ids, want)
	}
	if got["a"] != 1 || got["b"] != 0 || got["c"] != 2 {
		t.Errorf("resource counts = %v, want a:1 b:0 c:2", got)
	}
}
