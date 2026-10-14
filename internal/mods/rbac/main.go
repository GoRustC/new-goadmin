package rbac

import (
	"context"
	"fmt"
	"io"
	"path/filepath"

	"github.com/LyricTian/captcha"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"gorm.io/gorm"

	"github.com/GoRustC/new-goadmin/internal/config"
	"github.com/GoRustC/new-goadmin/internal/mods/rbac/api"
	"github.com/GoRustC/new-goadmin/internal/mods/rbac/schema"
	"github.com/GoRustC/new-goadmin/pkg/captchax"
	"github.com/GoRustC/new-goadmin/pkg/logging"
)

type RBAC struct {
	captchaStore io.Closer `wire:"-"`
	Config       *config.Config
	DB           *gorm.DB
	MenuAPI      *api.Menu
	RoleAPI      *api.Role
	UserAPI      *api.User
	LoginAPI     *api.Login
	LoggerAPI    *api.Logger
	Casbinx      *Casbinx
}

func (a *RBAC) AutoMigrate(ctx context.Context) error {
	if err := a.DB.AutoMigrate(
		new(schema.Menu),
		new(schema.MenuResource),
		new(schema.Role),
		new(schema.RoleMenu),
		new(schema.User),
		new(schema.UserRole),
		// The /loggers API reads this table. The database log hook also creates
		// it, but the API must not fail when that hook is disabled.
		new(schema.Logger),
	); err != nil {
		return fmt.Errorf("migrate rbac tables (a unique index fails if existing rows contain duplicates): %w", err)
	}
	return a.dropLegacyIndexes()
}

// dropLegacyIndexes removes non-unique indexes that the unique indexes on
// user.username, role.code and menu(parent_id, code) replace. Databases
// created before those unique indexes still have them.
func (a *RBAC) dropLegacyIndexes() error {
	m := a.DB.Migrator()
	ns := a.DB.NamingStrategy
	for _, legacy := range []struct {
		model  any
		table  string // struct name, mapped through the naming strategy
		column string
	}{
		{new(schema.User), "User", "username"},
		{new(schema.Role), "Role", "code"},
		{new(schema.Menu), "Menu", "code"},
	} {
		// The name gorm gave the old "index" tag, including any table prefix.
		name := ns.IndexName(ns.TableName(legacy.table), legacy.column)
		if m.HasIndex(legacy.model, name) {
			if err := m.DropIndex(legacy.model, name); err != nil {
				return fmt.Errorf("drop legacy index %s: %w", name, err)
			}
		}
	}
	return nil
}

func (a *RBAC) Init(ctx context.Context) error {
	a.initCaptchaStore()

	if a.Config.Storage.DB.AutoMigrate {
		if err := a.AutoMigrate(ctx); err != nil {
			return err
		}
	}

	a.LoggerAPI.LoggerBIZ.StartRetention(ctx)

	if name := a.Config.General.MenuFile; name != "" {
		fullPath := filepath.Join(a.Config.General.WorkDir, name)
		if err := a.MenuAPI.MenuBIZ.InitFromFile(ctx, fullPath); err != nil {
			logging.Context(ctx).Error("failed to init menu data", zap.Error(err), zap.String("file", fullPath))
		}
	}

	// Build the policy after importing menu.json, so resources added or
	// changed there apply from this start on.
	if err := a.Casbinx.Load(ctx); err != nil {
		return err
	}

	return nil
}

func (a *RBAC) RegisterV1Routers(ctx context.Context, v1 *gin.RouterGroup) error {
	captcha := v1.Group("captcha")
	{
		captcha.GET("id", a.LoginAPI.GetCaptcha)
		captcha.GET("image", a.LoginAPI.ResponseCaptcha)
	}

	v1.POST("login", a.LoginAPI.Login)

	current := v1.Group("current")
	{
		current.POST("refresh-token", a.LoginAPI.RefreshToken)
		current.GET("user", a.LoginAPI.GetUserInfo)
		current.GET("menus", a.LoginAPI.QueryMenus)
		current.PUT("password", a.LoginAPI.UpdatePassword)
		current.PUT("user", a.LoginAPI.UpdateUser)
		current.POST("logout", a.LoginAPI.Logout)
	}

	menu := v1.Group("menus")
	{
		menu.GET("", a.MenuAPI.Query)
		menu.GET(":id", a.MenuAPI.Get)
		menu.POST("", a.MenuAPI.Create)
		menu.PUT(":id", a.MenuAPI.Update)
		menu.DELETE(":id", a.MenuAPI.Delete)
	}

	role := v1.Group("roles")
	{
		role.GET("", a.RoleAPI.Query)
		role.GET(":id", a.RoleAPI.Get)
		role.POST("", a.RoleAPI.Create)
		role.PUT(":id", a.RoleAPI.Update)
		role.DELETE(":id", a.RoleAPI.Delete)
	}

	user := v1.Group("users")
	{
		user.GET("", a.UserAPI.Query)
		user.GET(":id", a.UserAPI.Get)
		user.POST("", a.UserAPI.Create)
		user.PUT(":id", a.UserAPI.Update)
		user.DELETE(":id", a.UserAPI.Delete)
		user.PATCH(":id/reset-pwd", a.UserAPI.ResetPassword)
	}

	logger := v1.Group("loggers")
	{
		logger.GET("", a.LoggerAPI.Query)
	}

	return nil
}

func (a *RBAC) Release(ctx context.Context) error {
	a.LoggerAPI.LoggerBIZ.StopRetention()
	if err := a.Casbinx.Release(ctx); err != nil {
		return err
	}
	if a.captchaStore != nil {
		return a.captchaStore.Close()
	}
	return nil
}

// initCaptchaStore replaces the captcha library's default store, which
// deadlocks under concurrent verification of the same captcha.
func (a *RBAC) initCaptchaStore() {
	cfg := a.Config.Util.Captcha
	if cfg.CacheType == "redis" {
		s := captchax.NewRedisStore(captchax.RedisConfig{
			Addr:      cfg.Redis.Addr,
			Username:  cfg.Redis.Username,
			Password:  cfg.Redis.Password,
			DB:        cfg.Redis.DB,
			KeyPrefix: a.Config.Storage.Cache.KeyPrefix + cfg.Redis.KeyPrefix,
		}, captcha.Expiration)
		a.captchaStore = s
		captcha.SetCustomStore(s)
		return
	}
	captcha.SetCustomStore(captchax.NewMemoryStore(captcha.Expiration))
}
