package logging

import (
	"context"

	"go.uber.org/zap"
)

// Tags classify log entries; they are stored in the tag column of the
// logger table.
const (
	TagKeyMain     = "main"
	TagKeyRecovery = "recovery"
	TagKeyRequest  = "request"
	TagKeyLogin    = "login"
	TagKeyLogout   = "logout"
	TagKeySystem   = "system"
	TagKeyOperate  = "operate"
)

type (
	ctxLoggerKey  struct{}
	ctxTraceIDKey struct{}
	ctxUserIDKey  struct{}
	ctxTagKey     struct{}
	ctxStackKey   struct{}
)

// NewLogger returns a copy of ctx carrying logger.
func NewLogger(ctx context.Context, logger *zap.Logger) context.Context {
	return context.WithValue(ctx, ctxLoggerKey{}, logger)
}

// FromLogger returns the logger stored by NewLogger, or the global logger.
func FromLogger(ctx context.Context) *zap.Logger {
	v := ctx.Value(ctxLoggerKey{})
	if v != nil {
		if vv, ok := v.(*zap.Logger); ok {
			return vv
		}
	}
	return zap.L()
}

// NewTraceID returns a copy of ctx whose log entries carry traceID.
func NewTraceID(ctx context.Context, traceID string) context.Context {
	return context.WithValue(ctx, ctxTraceIDKey{}, traceID)
}

// FromTraceID returns the trace ID stored by NewTraceID, or "".
func FromTraceID(ctx context.Context) string {
	v := ctx.Value(ctxTraceIDKey{})
	if v != nil {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

// NewUserID returns a copy of ctx whose log entries carry userID.
func NewUserID(ctx context.Context, userID string) context.Context {
	return context.WithValue(ctx, ctxUserIDKey{}, userID)
}

// FromUserID returns the user ID stored by NewUserID, or "".
func FromUserID(ctx context.Context) string {
	v := ctx.Value(ctxUserIDKey{})
	if v != nil {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

// NewTag returns a copy of ctx whose log entries carry tag.
func NewTag(ctx context.Context, tag string) context.Context {
	return context.WithValue(ctx, ctxTagKey{}, tag)
}

// FromTag returns the tag stored by NewTag, or "".
func FromTag(ctx context.Context) string {
	v := ctx.Value(ctxTagKey{})
	if v != nil {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

// NewStack returns a copy of ctx whose log entries carry the stack trace.
func NewStack(ctx context.Context, stack string) context.Context {
	return context.WithValue(ctx, ctxStackKey{}, stack)
}

// FromStack returns the stack trace stored by NewStack, or "".
func FromStack(ctx context.Context) string {
	v := ctx.Value(ctxStackKey{})
	if v != nil {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

// Context returns the logger of ctx with the trace ID, user ID, tag and stack
// stored in ctx as fields.
func Context(ctx context.Context) *zap.Logger {
	var fields []zap.Field
	if v := FromTraceID(ctx); v != "" {
		fields = append(fields, zap.String("trace_id", v))
	}
	if v := FromUserID(ctx); v != "" {
		fields = append(fields, zap.String("user_id", v))
	}
	if v := FromTag(ctx); v != "" {
		fields = append(fields, zap.String("tag", v))
	}
	if v := FromStack(ctx); v != "" {
		fields = append(fields, zap.String("stack", v))
	}
	return FromLogger(ctx).With(fields...)
}
