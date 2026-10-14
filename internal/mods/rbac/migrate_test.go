package rbac

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	gormschema "gorm.io/gorm/schema"

	"github.com/GoRustC/new-goadmin/internal/mods/rbac/schema"
)

// Tables as created before the unique indexes existed.
type legacyUser struct {
	ID       string `gorm:"size:20;primarykey"`
	Username string `gorm:"size:64;index"`
}
type legacyRole struct {
	ID   string `gorm:"size:20;primarykey"`
	Code string `gorm:"size:32;index"`
}
type legacyMenu struct {
	ID       string `gorm:"size:20;primarykey"`
	Code     string `gorm:"size:32;index"`
	ParentID string `gorm:"size:20;index"`
}

func TestAutoMigrateReplacesLegacyIndexes(t *testing.T) {
	for _, prefix := range []string{"", "app_"} {
		t.Run("prefix="+prefix, func(t *testing.T) {
			db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "x.db")), &gorm.Config{
				NamingStrategy: gormschema.NamingStrategy{TablePrefix: prefix, SingularTable: true},
				TranslateError: true,
			})
			if err != nil {
				t.Fatal(err)
			}
			for _, l := range []struct {
				table string
				model any
			}{{"user", new(legacyUser)}, {"role", new(legacyRole)}, {"menu", new(legacyMenu)}} {
				if err := db.Table(prefix + l.table).AutoMigrate(l.model); err != nil {
					t.Fatal(err)
				}
			}
			m := db.Migrator()
			if !m.HasIndex(new(schema.User), "idx_"+prefix+"user_username") {
				t.Fatal("test setup: legacy index missing")
			}

			if err := (&RBAC{DB: db}).AutoMigrate(context.Background()); err != nil {
				t.Fatal(err)
			}

			for _, gone := range []struct {
				model any
				name  string
			}{
				{new(schema.User), "idx_" + prefix + "user_username"},
				{new(schema.Role), "idx_" + prefix + "role_code"},
				{new(schema.Menu), "idx_" + prefix + "menu_code"},
			} {
				if m.HasIndex(gone.model, gone.name) {
					t.Errorf("legacy index %s still present", gone.name)
				}
			}
			for _, want := range []struct {
				model any
				name  string
			}{
				{new(schema.User), "uk_user_username"},
				{new(schema.Role), "uk_role_code"},
				{new(schema.Menu), "uk_menu_parent_code"},
			} {
				if !m.HasIndex(want.model, want.name) {
					t.Errorf("unique index %s missing", want.name)
				}
			}

			// The unique index is enforced.
			if err := db.Create(&schema.User{ID: "u1", Username: "same"}).Error; err != nil {
				t.Fatal(err)
			}
			if err := db.Create(&schema.User{ID: "u2", Username: "same"}).Error; !errors.Is(err, gorm.ErrDuplicatedKey) {
				t.Fatalf("duplicate username: got %v, want gorm.ErrDuplicatedKey", err)
			}
		})
	}
}
