// Package dal contains the data access of the rbac module.
package dal

import (
	"context"
	"time"

	"gorm.io/gorm"

	"github.com/GoRustC/new-goadmin/internal/mods/rbac/schema"
	"github.com/GoRustC/new-goadmin/pkg/util"
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

	// Filter and count on the logger table alone. The user name filter is a
	// subquery and user names are filled in only for the returned page, so
	// counting a large table never joins users.
	db := GetLoggerDB(ctx, a.DB)
	if v := params.Level; v != "" {
		db = db.Where("level = ?", v)
	}
	if v := params.LikeMessage; len(v) > 0 {
		db = db.Where(util.ContainsCondition("message", v))
	}
	if v := params.TraceID; v != "" {
		db = db.Where("trace_id = ?", v)
	}
	if v := params.LikeUserName; v != "" {
		users := GetUserDB(ctx, a.DB).Select("id").Where(util.ContainsCondition("username", v))
		db = db.Where("user_id IN (?)", users)
	}
	if v := params.Tag; v != "" {
		db = db.Where("tag = ?", v)
	}
	// Compared as timestamps, never as the raw query strings.
	if params.StartAt != nil {
		db = db.Where("created_at >= ?", *params.StartAt)
	}
	if params.EndAt != nil {
		db = db.Where("created_at <= ?", *params.EndAt)
	}

	var list schema.Loggers
	pageResult, err := util.WrapPageQuery(ctx, db, params.PaginationParam, opt.QueryOptions, &list)
	if err != nil {
		return nil, err
	}
	if err := a.fillUserNames(ctx, list); err != nil {
		return nil, err
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
	return result.RowsAffected, result.Error
}

// fillUserNames sets UserName and LoginName from the user table.
func (a *Logger) fillUserNames(ctx context.Context, list schema.Loggers) error {
	idSet := make(map[string]struct{})
	for _, l := range list {
		if l.UserID != "" {
			idSet[l.UserID] = struct{}{}
		}
	}
	if len(idSet) == 0 {
		return nil
	}
	ids := make([]string, 0, len(idSet))
	for id := range idSet {
		ids = append(ids, id)
	}
	var users schema.Users
	if err := GetUserDB(ctx, a.DB).Select("id", "name", "username").Where("id IN (?)", ids).Find(&users).Error; err != nil {
		return err
	}
	byID := make(map[string]*schema.User, len(users))
	for _, u := range users {
		byID[u.ID] = u
	}
	for _, l := range list {
		if u, ok := byID[l.UserID]; ok {
			l.UserName, l.LoginName = u.Name, u.Username
		}
	}
	return nil
}
