package util

import (
	"gorm.io/gorm/clause"

	"github.com/GoRustC/new-goadmin/pkg/errors"
)

const (
	// ReqBodyKey is the gin context key holding the raw request body copied
	// by the CopyBody middleware.
	ReqBodyKey = "req-body"
	// TreePathDelimiter separates the IDs in a tree path such as a menu's
	// parent path.
	TreePathDelimiter = "."
)

// ResponseResult is the JSON envelope of every API response.
type ResponseResult struct {
	Success bool          `json:"success"`
	Data    any           `json:"data,omitempty"`
	Total   int64         `json:"total,omitempty"`
	Error   *errors.Error `json:"error,omitempty"`
}

// PaginationResult describes the page returned by a paginated query.
type PaginationResult struct {
	Total    int64 `json:"total"`
	Current  int   `json:"current"`
	PageSize int   `json:"pageSize"`
}

// PaginationParam holds the paging parameters of a query. Pagination enables
// paging; OnlyCount returns the total without rows.
type PaginationParam struct {
	Pagination bool `form:"-"`
	OnlyCount  bool `form:"-"`
	Current    int  `form:"current"`
	PageSize   int  `form:"pageSize" binding:"max=100"`
}

// QueryOptions selects columns and ordering for a query.
type QueryOptions struct {
	SelectFields []string
	OmitFields   []string
	OrderFields  OrderByParams
}

// Direction is a sort direction.
type Direction string

// Sort directions.
const (
	ASC  Direction = "ASC"
	DESC Direction = "DESC"
)

// OrderByParam orders a query by one column.
type OrderByParam struct {
	Field     string
	Direction Direction
}

// OrderByParams orders a query by several columns, in order.
type OrderByParams []OrderByParam

// Clauses returns the ordering as gorm clauses. gorm quotes each column for
// the current database, so names that are reserved words (order, rank, key)
// work and a field name can never inject SQL. Unknown directions sort
// ascending.
func (a OrderByParams) Clauses() []clause.OrderByColumn {
	cols := make([]clause.OrderByColumn, 0, len(a))
	for _, v := range a {
		cols = append(cols, clause.OrderByColumn{
			Column: clause.Column{Name: v.Field},
			Desc:   v.Direction == DESC,
		})
	}
	return cols
}
