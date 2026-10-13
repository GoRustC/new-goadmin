package util

import (
	"context"
	"strings"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type Trans struct {
	DB *gorm.DB
}

type TransFunc func(context.Context) error

func (a *Trans) Exec(ctx context.Context, fn TransFunc) error {
	if _, ok := FromTrans(ctx); ok {
		return fn(ctx)
	}

	return a.DB.Transaction(func(db *gorm.DB) error {
		return fn(NewTrans(ctx, db))
	})
}

func GetDB(ctx context.Context, defDB *gorm.DB) *gorm.DB {
	db := defDB
	if tdb, ok := FromTrans(ctx); ok {
		db = tdb
	}
	if FromRowLock(ctx) {
		db = db.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	return db.WithContext(ctx)
}

// likeEscaper makes LIKE wildcards in user input match literally. "!" is the
// escape character because a backslash means different things in MySQL and
// PostgreSQL string literals.
var likeEscaper = strings.NewReplacer("!", "!!", "%", "!%", "_", "!_")

// ContainsCondition returns a WHERE condition and its argument matching rows
// whose column contains v, ignoring case on every database (PostgreSQL's LIKE
// is case-sensitive, MySQL's and SQLite's are not) and treating % and _ in v
// as plain characters.
func ContainsCondition(column, v string) (string, string) {
	return "LOWER(" + column + ") LIKE LOWER(?) ESCAPE '!'", "%" + likeEscaper.Replace(v) + "%"
}

// TableName returns the table name gorm uses for model, including the table
// prefix from the naming strategy, quoted for the current database. Use it
// when building raw joins: names such as "user" are reserved words in
// PostgreSQL and break unless quoted.
func TableName(db *gorm.DB, model any) (string, error) {
	stmt := &gorm.Statement{DB: db}
	if err := stmt.Parse(model); err != nil {
		return "", err
	}
	return db.Statement.Quote(stmt.Schema.Table), nil
}

func wrapQueryOptions(db *gorm.DB, opts QueryOptions) *gorm.DB {
	if len(opts.SelectFields) > 0 {
		db = db.Select(opts.SelectFields)
	}
	if len(opts.OmitFields) > 0 {
		db = db.Omit(opts.OmitFields...)
	}
	if len(opts.OrderFields) > 0 {
		db = db.Order(opts.OrderFields.ToSQL())
	}
	return db
}

func WrapPageQuery(ctx context.Context, db *gorm.DB, pp PaginationParam, opts QueryOptions, out interface{}) (*PaginationResult, error) {
	if pp.OnlyCount {
		var count int64
		err := db.Count(&count).Error
		if err != nil {
			return nil, err
		}
		return &PaginationResult{Total: count}, nil
	} else if !pp.Pagination {
		pageSize := pp.PageSize
		if pageSize > 0 {
			db = db.Limit(pageSize)
		}

		db = wrapQueryOptions(db, opts)
		err := db.Find(out).Error
		return nil, err
	}

	pp = normalizePage(pp)
	total, err := FindPage(ctx, db, pp, opts, out)
	if err != nil {
		return nil, err
	}

	return &PaginationResult{
		Total:    total,
		Current:  pp.Current,
		PageSize: pp.PageSize,
	}, nil
}

// DefaultPageSize is used when a paginated query asks for a page size of 0 or
// less, so a request can never fetch a whole table in one page.
const DefaultPageSize = 10

func normalizePage(pp PaginationParam) PaginationParam {
	if pp.Current < 1 {
		pp.Current = 1
	}
	if pp.PageSize < 1 {
		pp.PageSize = DefaultPageSize
	}
	return pp
}

func FindPage(ctx context.Context, db *gorm.DB, pp PaginationParam, opts QueryOptions, out interface{}) (int64, error) {
	pp = normalizePage(pp)
	db = db.WithContext(ctx)
	var count int64
	err := db.Count(&count).Error
	if err != nil {
		return 0, err
	} else if count == 0 {
		return count, nil
	}

	db = db.Offset((pp.Current - 1) * pp.PageSize).Limit(pp.PageSize)

	db = wrapQueryOptions(db, opts)
	err = db.Find(out).Error
	return count, err
}

func FindOne(ctx context.Context, db *gorm.DB, opts QueryOptions, out interface{}) (bool, error) {
	db = db.WithContext(ctx)
	db = wrapQueryOptions(db, opts)
	result := db.First(out)
	if err := result.Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func Exists(ctx context.Context, db *gorm.DB) (bool, error) {
	db = db.WithContext(ctx)
	var count int64
	result := db.Count(&count)
	if err := result.Error; err != nil {
		return false, err
	}
	return count > 0, nil
}
