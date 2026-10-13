package biz

import (
	"context"
	"sync"
	"time"

	"github.com/GoRustC/new-goadmin/internal/config"
	"github.com/GoRustC/new-goadmin/internal/mods/rbac/dal"
	"github.com/GoRustC/new-goadmin/internal/mods/rbac/schema"
	"github.com/GoRustC/new-goadmin/pkg/logging"
	"github.com/GoRustC/new-goadmin/pkg/util"
	"go.uber.org/zap"
)

// Logger management
type Logger struct {
	stop      chan struct{}  `wire:"-"`
	stopped   sync.WaitGroup `wire:"-"`
	Config    *config.Config
	LoggerDAL *dal.Logger
}

// loggerRetentionInterval is how often old log entries are deleted.
var loggerRetentionInterval = time.Hour

// StartRetention deletes log entries older than Dictionary.LoggerRetentionDays
// now and then every loggerRetentionInterval, until StopRetention.
func (a *Logger) StartRetention(ctx context.Context) {
	days := a.Config.Dictionary.LoggerRetentionDays
	if days <= 0 || a.stop != nil {
		return
	}
	a.stop = make(chan struct{})
	a.stopped.Add(1)
	go func() {
		defer a.stopped.Done()
		ticker := time.NewTicker(loggerRetentionInterval)
		defer ticker.Stop()
		for {
			cutoff := time.Now().AddDate(0, 0, -days)
			if n, err := a.LoggerDAL.DeleteBefore(ctx, cutoff); err != nil {
				logging.Context(ctx).Error("Failed to delete old log entries", zap.Error(err))
			} else if n > 0 {
				logging.Context(ctx).Info("Deleted old log entries", zap.Int64("rows", n), zap.Time("before", cutoff))
			}
			select {
			case <-ticker.C:
			case <-a.stop:
				return
			}
		}
	}()
}

// StopRetention stops the background deletion and waits for it to finish.
func (a *Logger) StopRetention() {
	if a.stop == nil {
		return
	}
	close(a.stop)
	a.stopped.Wait()
	a.stop = nil
}

// Query loggers from the data access object based on the provided parameters and options.
func (a *Logger) Query(ctx context.Context, params schema.LoggerQueryParam) (*schema.LoggerQueryResult, error) {
	params.Pagination = true

	result, err := a.LoggerDAL.Query(ctx, params, schema.LoggerQueryOptions{
		QueryOptions: util.QueryOptions{
			OrderFields: []util.OrderByParam{
				{Field: "created_at", Direction: util.DESC},
			},
		},
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}
