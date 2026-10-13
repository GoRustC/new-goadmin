package util

import (
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

type userRole struct {
	ID string
}

func TestTableNameAppliesPrefix(t *testing.T) {
	tests := []struct {
		prefix string
		want   string
	}{
		{prefix: "", want: "user_role"},
		{prefix: "app_", want: "app_user_role"},
	}
	for _, tt := range tests {
		db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
			NamingStrategy: schema.NamingStrategy{TablePrefix: tt.prefix, SingularTable: true},
		})
		if err != nil {
			t.Fatal(err)
		}
		got, err := TableName(db, new(userRole))
		if err != nil {
			t.Fatal(err)
		}
		if got != tt.want {
			t.Errorf("prefix %q: got %q, want %q", tt.prefix, got, tt.want)
		}
	}
}
