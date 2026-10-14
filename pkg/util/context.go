package util

import (
	"context"

	"gorm.io/gorm"

	"github.com/GoRustC/new-goadmin/pkg/encoding/json"
)

type (
	traceIDCtx    struct{}
	transCtx      struct{}
	rowLockCtx    struct{}
	userIDCtx     struct{}
	userTokenCtx  struct{}
	isRootUserCtx struct{}
	userCacheCtx  struct{}
)

// NewTraceID returns a copy of ctx carrying the request trace ID.
func NewTraceID(ctx context.Context, traceID string) context.Context {
	return context.WithValue(ctx, traceIDCtx{}, traceID)
}

// FromTraceID returns the trace ID stored by NewTraceID, or "".
func FromTraceID(ctx context.Context) string {
	v := ctx.Value(traceIDCtx{})
	if v != nil {
		return v.(string)
	}
	return ""
}

// NewTrans returns a copy of ctx carrying the transaction db, so data access
// in that context runs inside the transaction. See GetDB.
func NewTrans(ctx context.Context, db *gorm.DB) context.Context {
	return context.WithValue(ctx, transCtx{}, db)
}

// FromTrans returns the transaction stored by NewTrans.
func FromTrans(ctx context.Context) (*gorm.DB, bool) {
	v := ctx.Value(transCtx{})
	if v != nil {
		return v.(*gorm.DB), true
	}
	return nil, false
}

// NewRowLock returns a copy of ctx that makes GetDB lock the selected rows
// (SELECT ... FOR UPDATE).
func NewRowLock(ctx context.Context) context.Context {
	return context.WithValue(ctx, rowLockCtx{}, true)
}

// FromRowLock reports whether ctx was marked by NewRowLock.
func FromRowLock(ctx context.Context) bool {
	v := ctx.Value(rowLockCtx{})
	return v != nil && v.(bool)
}

// NewUserID returns a copy of ctx carrying the ID of the authenticated user.
func NewUserID(ctx context.Context, userID string) context.Context {
	return context.WithValue(ctx, userIDCtx{}, userID)
}

// FromUserID returns the user ID stored by NewUserID, or "".
func FromUserID(ctx context.Context) string {
	v := ctx.Value(userIDCtx{})
	if v != nil {
		return v.(string)
	}
	return ""
}

// NewUserToken returns a copy of ctx carrying the request's access token.
func NewUserToken(ctx context.Context, userToken string) context.Context {
	return context.WithValue(ctx, userTokenCtx{}, userToken)
}

// FromUserToken returns the token stored by NewUserToken, or "".
func FromUserToken(ctx context.Context) string {
	v := ctx.Value(userTokenCtx{})
	if v != nil {
		return v.(string)
	}
	return ""
}

// NewIsRootUser returns a copy of ctx that marks the user as root.
func NewIsRootUser(ctx context.Context) context.Context {
	return context.WithValue(ctx, isRootUserCtx{}, true)
}

// FromIsRootUser reports whether ctx was marked by NewIsRootUser.
func FromIsRootUser(ctx context.Context) bool {
	v := ctx.Value(isRootUserCtx{})
	return v != nil && v.(bool)
}

// UserCache is the per-user data cached between requests.
type UserCache struct {
	RoleIDs []string `json:"rids"`
}

// ParseUserCache decodes a value produced by UserCache.String. An empty or
// invalid value yields the zero UserCache.
func ParseUserCache(s string) UserCache {
	var a UserCache
	if s == "" {
		return a
	}

	_ = json.Unmarshal([]byte(s), &a)
	return a
}

// String encodes the cache entry as JSON.
func (a UserCache) String() string {
	return json.MarshalToString(a)
}

// NewUserCache returns a copy of ctx carrying the user's cache entry.
func NewUserCache(ctx context.Context, userCache UserCache) context.Context {
	return context.WithValue(ctx, userCacheCtx{}, userCache)
}

// FromUserCache returns the entry stored by NewUserCache, or the zero value.
func FromUserCache(ctx context.Context) UserCache {
	v := ctx.Value(userCacheCtx{})
	if v != nil {
		return v.(UserCache)
	}
	return UserCache{}
}
