package util

import (
	"context"
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
		{prefix: "", want: "`user_role`"},
		{prefix: "app_", want: "`app_user_role`"},
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

type item struct {
	ID int
}

func TestWrapPageQueryBounds(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(new(item)); err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 25; i++ {
		if err := db.Create(&item{ID: i}).Error; err != nil {
			t.Fatal(err)
		}
	}

	tests := []struct {
		name        string
		pp          PaginationParam
		wantLen     int
		wantFirst   int
		wantCurrent int
		wantSize    int
	}{
		{"zero page size uses default", PaginationParam{Pagination: true, Current: 1, PageSize: 0}, DefaultPageSize, 1, 1, DefaultPageSize},
		{"negative page size uses default", PaginationParam{Pagination: true, Current: 1, PageSize: -5}, DefaultPageSize, 1, 1, DefaultPageSize},
		{"zero current is first page", PaginationParam{Pagination: true, Current: 0, PageSize: 5}, 5, 1, 1, 5},
		{"explicit page", PaginationParam{Pagination: true, Current: 3, PageSize: 5}, 5, 11, 3, 5},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out []item
			res, err := WrapPageQuery(context.Background(), db.Model(new(item)).Order("id"), tt.pp, QueryOptions{}, &out)
			if err != nil {
				t.Fatal(err)
			}
			if len(out) != tt.wantLen || out[0].ID != tt.wantFirst {
				t.Fatalf("got %d rows starting at %d, want %d starting at %d", len(out), out[0].ID, tt.wantLen, tt.wantFirst)
			}
			if res.Total != 25 || res.Current != tt.wantCurrent || res.PageSize != tt.wantSize {
				t.Fatalf("got total=%d current=%d pageSize=%d", res.Total, res.Current, res.PageSize)
			}
		})
	}
}

// A column named after a reserved word used to break the ORDER BY, which was
// assembled as raw text.
type ranked struct {
	ID    int
	Order int `gorm:"column:order"`
}

func TestOrderByQuotesColumns(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(new(ranked)); err != nil {
		t.Fatal(err)
	}
	for i, o := range []int{3, 1, 2} {
		db.Create(&ranked{ID: i + 1, Order: o})
	}
	var out []ranked
	_, err = WrapPageQuery(context.Background(), db.Model(new(ranked)),
		PaginationParam{Pagination: true, Current: 1, PageSize: 10},
		QueryOptions{OrderFields: OrderByParams{{Field: "order", Direction: DESC}}}, &out)
	if err != nil {
		t.Fatalf("ORDER BY a reserved word failed: %v", err)
	}
	if len(out) != 3 || out[0].Order != 3 || out[2].Order != 1 {
		t.Fatalf("not sorted by order DESC: %+v", out)
	}
}
