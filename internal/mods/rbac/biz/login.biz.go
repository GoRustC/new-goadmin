package biz

import (
	"context"
	"crypto/subtle"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/LyricTian/captcha"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/GoRustC/new-goadmin/internal/config"
	"github.com/GoRustC/new-goadmin/internal/mods/rbac/dal"
	"github.com/GoRustC/new-goadmin/internal/mods/rbac/schema"
	"github.com/GoRustC/new-goadmin/pkg/cachex"
	"github.com/GoRustC/new-goadmin/pkg/crypto/hash"
	"github.com/GoRustC/new-goadmin/pkg/errors"
	"github.com/GoRustC/new-goadmin/pkg/jwtx"
	"github.com/GoRustC/new-goadmin/pkg/logging"
	"github.com/GoRustC/new-goadmin/pkg/util"
)

// Login management for RBAC
type Login struct {
	Config      *config.Config
	Cache       cachex.Cacher
	Auth        jwtx.Auther
	UserDAL     *dal.User
	UserRoleDAL *dal.UserRole
	MenuDAL     *dal.Menu
	UserBIZ     *User
}

func (a *Login) ParseUserID(c *gin.Context) (string, error) {
	rootID := a.Config.General.Root.ID
	if a.Config.Middleware.Auth.Disable {
		return rootID, nil
	}

	invalidToken := errors.Unauthorized(config.ErrInvalidTokenID, "Invalid access token")
	token := util.GetToken(c)
	if token == "" {
		return "", invalidToken
	}

	ctx := c.Request.Context()
	ctx = util.NewUserToken(ctx, token)

	claims, err := a.Auth.ParseClaims(ctx, token)
	if err != nil {
		if err == jwtx.ErrInvalidToken {
			return "", invalidToken
		}
		return "", err
	}
	userID := claims.Subject
	if a.sessionExpired(claims) {
		return "", errors.Unauthorized(config.ErrSessionExpiredID, "Session expired, please log in again")
	}
	if userID == rootID {
		c.Request = c.Request.WithContext(util.NewIsRootUser(ctx))
		return userID, nil
	}

	// Tokens issued before a password change or reset are no longer valid.
	if claims.IssuedAt != nil {
		if revoked, err := tokenRevoked(ctx, a.Cache, userID, claims.IssuedAt.Time); err != nil {
			return "", err
		} else if revoked {
			return "", invalidToken
		}
	}

	userCacheVal, ok, err := a.Cache.Get(ctx, config.CacheNSForUser, userID)
	if err != nil {
		return "", err
	} else if ok {
		userCache := util.ParseUserCache(userCacheVal)
		c.Request = c.Request.WithContext(util.NewUserCache(ctx, userCache))
		return userID, nil
	}

	// Check user status, if not activated, force to logout
	user, err := a.UserDAL.Get(ctx, userID, schema.UserQueryOptions{
		QueryOptions: util.QueryOptions{SelectFields: []string{"status"}},
	})
	if err != nil {
		return "", err
	} else if user == nil || user.Status != schema.UserStatusActivated {
		return "", invalidToken
	}

	roleIDs, err := a.UserBIZ.GetRoleIDs(ctx, userID)
	if err != nil {
		return "", err
	}

	userCache := util.UserCache{
		RoleIDs: roleIDs,
	}
	// Expire like the entry written at login, so a stale entry cannot outlive
	// the configured period even if an invalidation is missed.
	err = a.Cache.Set(ctx, config.CacheNSForUser, userID, userCache.String(),
		time.Duration(a.Config.Dictionary.UserCacheExp)*time.Hour)
	if err != nil {
		return "", err
	}

	c.Request = c.Request.WithContext(util.NewUserCache(ctx, userCache))
	return userID, nil
}

// This function generates a new captcha ID and returns it as a `schema.Captcha` struct. The length of
// the captcha is determined by the `Util.Captcha.Length` configuration value.
func (a *Login) GetCaptcha(ctx context.Context) (*schema.Captcha, error) {
	return &schema.Captcha{
		CaptchaID: captcha.NewLen(a.Config.Util.Captcha.Length),
	}, nil
}

// Response captcha image
func (a *Login) ResponseCaptcha(ctx context.Context, w http.ResponseWriter, id string, reload bool) error {
	if reload && !captcha.Reload(id) {
		return errors.NotFound("", "Captcha id not found")
	}

	// Headers must be set before the image is written; once the body starts,
	// later header changes are ignored.
	h := w.Header()
	h.Set("Cache-Control", "no-cache, no-store, must-revalidate")
	h.Set("Pragma", "no-cache")
	h.Set("Expires", "0")
	h.Set("Content-Type", "image/png")

	err := captcha.WriteImage(w, id, a.Config.Util.Captcha.Width, a.Config.Util.Captcha.Height)
	if err != nil {
		// Nothing was written yet; let the JSON error set its own content type.
		h.Del("Content-Type")
		if err == captcha.ErrNotFound {
			return errors.NotFound("", "Captcha id not found")
		}
		return err
	}
	return nil
}

// genUserToken issues a token for a session that began at sessionStart.
func (a *Login) genUserToken(ctx context.Context, userID string, sessionStart time.Time) (*schema.LoginToken, error) {
	token, err := a.Auth.GenerateToken(ctx, userID, jwtx.WithAuthTime(sessionStart))
	if err != nil {
		return nil, err
	}

	return &schema.LoginToken{
		AccessToken: token.GetAccessToken(),
		TokenType:   token.GetTokenType(),
		ExpiresAt:   token.GetExpiresAt(),
	}, nil
}

func (a *Login) Login(ctx context.Context, formItem *schema.LoginForm) (*schema.LoginToken, error) {
	// verify captcha
	if !captcha.VerifyString(formItem.CaptchaID, formItem.CaptchaCode) {
		return nil, errors.BadRequest(config.ErrInvalidCaptchaID, "Incorrect captcha")
	}

	ctx = logging.NewTag(ctx, logging.TagKeyLogin)

	lock := loginLock{cache: a.Cache, cfg: a.Config}
	if err := lock.check(ctx, formItem.Username); err != nil {
		logging.Context(ctx).Warn("Login rejected: account name locked", zap.String("username", formItem.Username))
		return nil, err
	}
	badCredentials := func() error {
		if err := lock.fail(ctx, formItem.Username); err != nil {
			logging.Context(ctx).Error("Failed to record login failure", zap.Error(err))
		}
		logging.Context(ctx).Warn("Login failed", zap.String("username", formItem.Username))
		return errors.BadRequest(config.ErrInvalidUsernameOrPassword, "Incorrect username or password")
	}

	// login by root
	if formItem.Username == a.Config.General.Root.Username {
		if !verifyRootPassword(a.Config.General.Root.Password, formItem.Password) {
			return nil, badCredentials()
		}
		_ = lock.reset(ctx, formItem.Username)

		userID := a.Config.General.Root.ID
		ctx = logging.NewUserID(ctx, userID)
		logging.Context(ctx).Info("Login by root")
		return a.genUserToken(ctx, userID, time.Now())
	}

	// get user info
	user, err := a.UserDAL.GetByUsername(ctx, formItem.Username, schema.UserQueryOptions{
		QueryOptions: util.QueryOptions{
			SelectFields: []string{"id", "password", "status"},
		},
	})
	if err != nil {
		return nil, err
	} else if user == nil {
		// Spend the same time as a wrong password, so timing does not reveal
		// whether the account exists.
		_ = hash.CompareHashAndPassword(dummyPasswordHash, formItem.Password)
		return nil, badCredentials()
	}

	// Check the password before the status, so the "not activated" message
	// cannot be used to discover frozen accounts without their password.
	if err := hash.CompareHashAndPassword(user.Password, formItem.Password); err != nil {
		return nil, badCredentials()
	}
	_ = lock.reset(ctx, formItem.Username)
	if user.Status != schema.UserStatusActivated {
		return nil, errors.BadRequest("", "User status is not activated, please contact the administrator")
	}

	userID := user.ID
	ctx = logging.NewUserID(ctx, userID)

	// set user cache with role ids
	roleIDs, err := a.UserBIZ.GetRoleIDs(ctx, userID)
	if err != nil {
		return nil, err
	}

	userCache := util.UserCache{RoleIDs: roleIDs}
	err = a.Cache.Set(ctx, config.CacheNSForUser, userID, userCache.String(),
		time.Duration(a.Config.Dictionary.UserCacheExp)*time.Hour)
	if err != nil {
		logging.Context(ctx).Error("Failed to set cache", zap.Error(err))
	}
	logging.Context(ctx).Info("Login success", zap.String("username", formItem.Username))

	// generate token
	return a.genUserToken(ctx, userID, time.Now())
}

func (a *Login) RefreshToken(ctx context.Context) (*schema.LoginToken, error) {
	userID := util.FromUserID(ctx)

	if !util.FromIsRootUser(ctx) {
		user, err := a.UserDAL.Get(ctx, userID, schema.UserQueryOptions{
			QueryOptions: util.QueryOptions{
				SelectFields: []string{"status"},
			},
		})
		if err != nil {
			return nil, err
		} else if user == nil {
			return nil, errors.BadRequest("", "Incorrect user")
		} else if user.Status != schema.UserStatusActivated {
			return nil, errors.BadRequest("", "User status is not activated, please contact the administrator")
		}
	}

	// Keep the original login time, so refreshing cannot extend a session
	// beyond MaxSessionHours.
	sessionStart := time.Now()
	if old := util.FromUserToken(ctx); old != "" {
		claims, err := a.Auth.ParseClaims(ctx, old)
		if err != nil {
			return nil, errors.Unauthorized(config.ErrInvalidTokenID, "Invalid access token")
		}
		sessionStart = claims.SessionStart()
	}
	token, err := a.genUserToken(ctx, userID, sessionStart)
	if err != nil {
		return nil, err
	}
	// Retire the token used for the refresh, so refreshing replaces a token
	// instead of adding a second valid one.
	if old := util.FromUserToken(ctx); old != "" {
		if err := a.Auth.DestroyToken(ctx, old); err != nil {
			logging.Context(ctx).Error("Failed to revoke refreshed token", zap.Error(err))
		}
	}
	return token, nil
}

func (a *Login) Logout(ctx context.Context) error {
	userToken := util.FromUserToken(ctx)
	if userToken == "" {
		return nil
	}

	ctx = logging.NewTag(ctx, logging.TagKeyLogout)
	if err := a.Auth.DestroyToken(ctx, userToken); err != nil {
		return err
	}

	userID := util.FromUserID(ctx)
	err := a.Cache.Delete(ctx, config.CacheNSForUser, userID)
	if err != nil {
		logging.Context(ctx).Error("Failed to delete user cache", zap.Error(err))
	}
	logging.Context(ctx).Info("Logout success")

	return nil
}

// Get user info
func (a *Login) GetUserInfo(ctx context.Context) (*schema.User, error) {
	if util.FromIsRootUser(ctx) {
		return &schema.User{
			ID:       a.Config.General.Root.ID,
			Username: a.Config.General.Root.Username,
			Name:     a.Config.General.Root.Name,
			Status:   schema.UserStatusActivated,
		}, nil
	}

	userID := util.FromUserID(ctx)
	user, err := a.UserDAL.Get(ctx, userID, schema.UserQueryOptions{
		QueryOptions: util.QueryOptions{
			OmitFields: []string{"password"},
		},
	})
	if err != nil {
		return nil, err
	} else if user == nil {
		return nil, errors.NotFound("", "User not found")
	}

	userRoleResult, err := a.UserRoleDAL.Query(ctx, schema.UserRoleQueryParam{
		UserID: userID,
	}, schema.UserRoleQueryOptions{
		JoinRole: true,
	})
	if err != nil {
		return nil, err
	}
	user.Roles = userRoleResult.Data

	return user, nil
}

// Change login password
func (a *Login) UpdatePassword(ctx context.Context, updateItem *schema.UpdateLoginPassword) error {
	if util.FromIsRootUser(ctx) {
		return errors.BadRequest("", "Root user cannot change password")
	}

	userID := util.FromUserID(ctx)
	user, err := a.UserDAL.Get(ctx, userID, schema.UserQueryOptions{
		QueryOptions: util.QueryOptions{
			SelectFields: []string{"password"},
		},
	})
	if err != nil {
		return err
	} else if user == nil {
		return errors.NotFound("", "User not found")
	}

	// check old password
	if err := hash.CompareHashAndPassword(user.Password, updateItem.OldPassword); err != nil {
		return errors.BadRequest("", "Incorrect old password")
	}

	// update password
	newPassword, err := hash.GeneratePassword(updateItem.NewPassword)
	if err != nil {
		return err
	}
	if err := a.UserDAL.UpdatePasswordByID(ctx, userID, newPassword); err != nil {
		return err
	}
	// Sign out every session, including the current one.
	return revokeUserTokens(ctx, a.Cache, a.Config, userID)
}

// Query menus based on user permissions
func (a *Login) QueryMenus(ctx context.Context) (schema.Menus, error) {
	menuQueryParams := schema.MenuQueryParam{
		Status: schema.MenuStatusEnabled,
	}

	isRoot := util.FromIsRootUser(ctx)
	if !isRoot {
		menuQueryParams.UserID = util.FromUserID(ctx)
	}
	menuResult, err := a.MenuDAL.Query(ctx, menuQueryParams, schema.MenuQueryOptions{
		QueryOptions: util.QueryOptions{
			OrderFields: schema.MenusOrderParams,
		},
	})
	if err != nil {
		return nil, err
	} else if isRoot {
		return menuResult.Data.ToTree(), nil
	}

	// fill parent menus
	if parentIDs := menuResult.Data.SplitParentIDs(); len(parentIDs) > 0 {
		var missMenusIDs []string
		menuIDMapper := menuResult.Data.ToMap()
		for _, parentID := range parentIDs {
			if _, ok := menuIDMapper[parentID]; !ok {
				missMenusIDs = append(missMenusIDs, parentID)
			}
		}
		if len(missMenusIDs) > 0 {
			parentResult, err := a.MenuDAL.Query(ctx, schema.MenuQueryParam{
				InIDs: missMenusIDs,
			})
			if err != nil {
				return nil, err
			}
			menuResult.Data = append(menuResult.Data, parentResult.Data...)
			sort.Sort(menuResult.Data)
		}
	}

	return menuResult.Data.ToTree(), nil
}

// Update current user info
func (a *Login) UpdateUser(ctx context.Context, updateItem *schema.UpdateCurrentUser) error {
	if util.FromIsRootUser(ctx) {
		return errors.BadRequest("", "Root user cannot update")
	}

	userID := util.FromUserID(ctx)
	user, err := a.UserDAL.Get(ctx, userID)
	if err != nil {
		return err
	} else if user == nil {
		return errors.NotFound("", "User not found")
	}

	user.Name = updateItem.Name
	user.Phone = updateItem.Phone
	user.Email = updateItem.Email
	user.Remark = updateItem.Remark
	return a.UserDAL.Update(ctx, user, "name", "phone", "email", "remark")
}

// verifyRootPassword checks the root password from config. It accepts either
// a bcrypt hash or the plain value, compared in constant time. An empty
// configured password disables root login.
func verifyRootPassword(configured, given string) bool {
	if configured == "" || given == "" {
		return false
	}
	if strings.HasPrefix(configured, "$2a$") || strings.HasPrefix(configured, "$2b$") || strings.HasPrefix(configured, "$2y$") {
		return hash.CompareHashAndPassword(configured, given) == nil
	}
	return subtle.ConstantTimeCompare([]byte(configured), []byte(given)) == 1
}

// sessionExpired reports whether the session behind claims is older than
// Middleware.Auth.MaxSessionHours.
func (a *Login) sessionExpired(claims *jwtx.Claims) bool {
	max := a.Config.Middleware.Auth.MaxSessionHours
	if max <= 0 {
		return false
	}
	start := claims.SessionStart()
	return !start.IsZero() && time.Since(start) > time.Duration(max)*time.Hour
}
