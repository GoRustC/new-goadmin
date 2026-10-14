package util

import (
	"github.com/GoRustC/new-goadmin/pkg/errors"
	"gorm.io/gorm/clause"
)

const (
	ReqBodyKey        = "req-body"
	ResBodyKey        = "res-body"
	TreePathDelimiter = "."
)

type ResponseResult struct {
	Success bool          `json:"success"`
	Data    interface{}   `json:"data,omitempty"`
	Total   int64         `json:"total,omitempty"`
	Error   *errors.Error `json:"error,omitempty"`
}

type PaginationResult struct {
	Total    int64 `json:"total"`
	Current  int   `json:"current"`
	PageSize int   `json:"pageSize"`
}

type PaginationParam struct {
	Pagination bool `form:"-"`
	OnlyCount  bool `form:"-"`
	Current    int  `form:"current"`
	PageSize   int  `form:"pageSize" binding:"max=100"`
}

type QueryOptions struct {
	SelectFields []string
	OmitFields   []string
	OrderFields  OrderByParams
}

type Direction string

const (
	ASC  Direction = "ASC"
	DESC Direction = "DESC"
)

type OrderByParam struct {
	Field     string
	Direction Direction
}

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
