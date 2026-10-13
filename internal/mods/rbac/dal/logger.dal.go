package dal

import (
	"context"
	"fmt"
	"time"

	"github.com/GoRustC/new-goadmin/internal/mods/rbac/schema"
	"github.com/GoRustC/new-goadmin/pkg/errors"
	"github.com/GoRustC/new-goadmin/pkg/util"
	"gorm.io/gorm"
)

// Get logger storage instance
func GetLoggerDB(ctx context.Context, defDB *gorm.DB) *gorm.DB {
	return util.GetDB(ctx, defDB).Model(new(schema.Logger))
}

// Logger management
type Logger struct {
	DB *gorm.DB
}

// Query loggers from the database based on the provided parameters and options.
func (a *Logger) Query(ctx context.Context, params schema.LoggerQueryParam, opts ...schema.LoggerQueryOptions) (*schema.LoggerQueryResult, error) {
	var opt schema.LoggerQueryOptions
	if len(opts) > 0 {
		opt = opts[0]
	}

	loggerTable, err := util.TableName(a.DB, new(schema.Logger))
	if err != nil {
		return nil, errors.WithStack(err)
	}
	userTable, err := util.TableName(a.DB, new(schema.User))
	if err != nil {
		return nil, errors.WithStack(err)
	}

	// util.GetDB keeps the query inside the caller's transaction, if any.
	db := util.GetDB(ctx, a.DB).Table(fmt.Sprintf("%s AS a", loggerTable))
	db = db.Joins(fmt.Sprintf("left join %s b on a.user_id=b.id", userTable))
	db = db.Select("a.*,b.name as user_name,b.username as login_name")

	if v := params.Level; v != "" {
		db = db.Where("a.level = ?", v)
	}
	if v := params.LikeMessage; len(v) > 0 {
		db = db.Where(util.ContainsCondition("a.message", v))
	}
	if v := params.TraceID; v != "" {
		db = db.Where("a.trace_id = ?", v)
	}
	if v := params.LikeUserName; v != "" {
		db = db.Where(util.ContainsCondition("b.username", v))
	}
	if v := params.Tag; v != "" {
		db = db.Where("a.tag = ?", v)
	}
	if start, end := params.StartTime, params.EndTime; start != "" && end != "" {
		db = db.Where("a.created_at BETWEEN ? AND ?", start, end)
	}

	var list schema.Loggers
	pageResult, err := util.WrapPageQuery(ctx, db, params.PaginationParam, opt.QueryOptions, &list)
	if err != nil {
		return nil, errors.WithStack(err)
	}

	queryResult := &schema.LoggerQueryResult{
		PageResult: pageResult,
		Data:       list,
	}
	return queryResult, nil
}

// DeleteBefore removes log entries created before t and returns how many.
func (a *Logger) DeleteBefore(ctx context.Context, t time.Time) (int64, error) {
	result := GetLoggerDB(ctx, a.DB).Where("created_at < ?", t).Delete(new(schema.Logger))
	return result.RowsAffected, errors.WithStack(result.Error)
}
