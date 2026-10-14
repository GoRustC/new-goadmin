package bootstrap

import (
	"context"

	"github.com/spf13/cast"

	"github.com/GoRustC/new-goadmin/internal/config"
	"github.com/GoRustC/new-goadmin/pkg/gormx"
	"github.com/GoRustC/new-goadmin/pkg/logging"
)

func initLoggerHook(_ context.Context, cfg *logging.HookConfig) (*logging.Hook, error) {
	extra := cfg.Extra
	if extra == nil {
		extra = make(map[string]string)
	}
	extra["appname"] = config.C.General.AppName

	switch cfg.Type {
	case "gorm":
		db, err := gormx.New(gormx.Config{
			Debug:        cast.ToBool(cfg.Options["Debug"]),
			DBType:       cast.ToString(cfg.Options["DBType"]),
			DSN:          cast.ToString(cfg.Options["DSN"]),
			MaxLifetime:  cast.ToInt(cfg.Options["MaxLifetime"]),
			MaxIdleTime:  cast.ToInt(cfg.Options["MaxIdleTime"]),
			MaxOpenConns: cast.ToInt(cfg.Options["MaxOpenConns"]),
			MaxIdleConns: cast.ToInt(cfg.Options["MaxIdleConns"]),
			TablePrefix:  config.C.Storage.DB.TablePrefix,
			// Logging a failed log write would queue another write.
			DisableSQLLog: true,
		})
		if err != nil {
			return nil, err
		}

		gormHook, err := logging.NewGormHook(db)
		if err != nil {
			return nil, err
		}
		hook := logging.NewHook(gormHook,
			logging.SetHookExtra(extra),
			logging.SetHookMaxJobs(cfg.MaxBuffer),
			logging.SetHookMaxWorkers(cfg.MaxThread))
		return hook, nil
	default:
		return nil, nil
	}
}
