package rbac

import (
	"context"
	"io"
	"path/filepath"

	"github.com/LyricTian/captcha"
	"github.com/LyricTian/gin-admin/v10/internal/config"
	"github.com/LyricTian/gin-admin/v10/internal/mods/rbac/api"
	"github.com/LyricTian/gin-admin/v10/internal/mods/rbac/schema"
	"github.com/LyricTian/gin-admin/v10/pkg/captchax"
	"github.com/LyricTian/gin-admin/v10/pkg/logging"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"gorm.io/gorm"
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
	return a.DB.AutoMigrate(
		new(schema.Menu),
		new(schema.MenuResource),
		new(schema.Role),
		new(schema.RoleMenu),
		new(schema.User),
		new(schema.UserRole),
		// The /loggers API reads this table. The database log hook also creates
		// it, but the API must not fail when that hook is disabled.
		new(schema.Logger),
	)
}

func (a *RBAC) Init(ctx context.Context) error {
	a.initCaptchaStore()

	if a.Config.Storage.DB.AutoMigrate {
		if err := a.AutoMigrate(ctx); err != nil {
			return err
		}
	}

	if err := a.Casbinx.Load(ctx); err != nil {
		return err
	}
	a.LoggerAPI.LoggerBIZ.StartRetention(ctx)

	if name := a.Config.General.MenuFile; name != "" {
		fullPath := filepath.Join(a.Config.General.WorkDir, name)
		if err := a.MenuAPI.MenuBIZ.InitFromFile(ctx, fullPath); err != nil {
			logging.Context(ctx).Error("failed to init menu data", zap.Error(err), zap.String("file", fullPath))
		}
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
			KeyPrefix: cfg.Redis.KeyPrefix,
		}, captcha.Expiration)
		a.captchaStore = s
		captcha.SetCustomStore(s)
		return
	}
	captcha.SetCustomStore(captchax.NewMemoryStore(captcha.Expiration))
}
