package biz

import (
	"context"
	"time"

	"github.com/GoRustC/new-goadmin/internal/config"
	"github.com/GoRustC/new-goadmin/internal/mods/rbac/dal"
	"github.com/GoRustC/new-goadmin/internal/mods/rbac/schema"
	"github.com/GoRustC/new-goadmin/pkg/cachex"
	"github.com/GoRustC/new-goadmin/pkg/crypto/hash"
	"github.com/GoRustC/new-goadmin/pkg/errors"
	"github.com/GoRustC/new-goadmin/pkg/util"
)

// User management for RBAC
type User struct {
	Config      *config.Config
	Cache       cachex.Cacher
	Trans       *util.Trans
	UserDAL     *dal.User
	UserRoleDAL *dal.UserRole
	RoleMenuDAL *dal.RoleMenu
}

func (a *User) guard() privilegeGuard {
	return privilegeGuard{userRoles: a.UserRoleDAL, roleMenus: a.RoleMenuDAL}
}

// Query users from the data access object based on the provided parameters and options.
func (a *User) Query(ctx context.Context, params schema.UserQueryParam) (*schema.UserQueryResult, error) {
	params.Pagination = true

	result, err := a.UserDAL.Query(ctx, params, schema.UserQueryOptions{
		QueryOptions: util.QueryOptions{
			OrderFields: []util.OrderByParam{
				{Field: "created_at", Direction: util.DESC},
			},
			OmitFields: []string{"password"},
		},
	})
	if err != nil {
		return nil, err
	}

	if userIDs := result.Data.ToIDs(); len(userIDs) > 0 {
		userRoleResult, err := a.UserRoleDAL.Query(ctx, schema.UserRoleQueryParam{
			InUserIDs: userIDs,
		}, schema.UserRoleQueryOptions{
			JoinRole: true,
		})
		if err != nil {
			return nil, err
		}
		userRolesMap := userRoleResult.Data.ToUserIDMap()
		for _, user := range result.Data {
			user.Roles = userRolesMap[user.ID]
		}
	}

	return result, nil
}

// Get the specified user from the data access object.
func (a *User) Get(ctx context.Context, id string) (*schema.User, error) {
	user, err := a.UserDAL.Get(ctx, id, schema.UserQueryOptions{
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
		UserID: id,
	})
	if err != nil {
		return nil, err
	}
	user.Roles = userRoleResult.Data

	return user, nil
}

// Create a new user in the data access object.
func (a *User) Create(ctx context.Context, formItem *schema.UserForm) (*schema.User, error) {
	if err := a.guard().checkAssignRoles(ctx, roleIDsOf(formItem.Roles)); err != nil {
		return nil, err
	}

	existsUsername, err := a.UserDAL.ExistsUsername(ctx, formItem.Username)
	if err != nil {
		return nil, err
	} else if existsUsername {
		return nil, errors.BadRequest("", "Username already exists")
	}

	user := &schema.User{
		ID:        util.NewXID(),
		CreatedAt: time.Now(),
	}

	if formItem.Password == "" {
		if a.Config.General.DefaultLoginPwd == "" {
			return nil, errors.BadRequest("", "Password is required")
		}
		formItem.Password = a.Config.General.DefaultLoginPwd
	}

	if err := formItem.FillTo(user); err != nil {
		return nil, err
	}

	err = a.Trans.Exec(ctx, func(ctx context.Context) error {
		if err := a.UserDAL.Create(ctx, user); err != nil {
			return err
		}

		for _, userRole := range formItem.Roles {
			userRole.ID = util.NewXID()
			userRole.UserID = user.ID
			userRole.CreatedAt = time.Now()
			if err := a.UserRoleDAL.Create(ctx, userRole); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	user.Roles = formItem.Roles

	return user, nil
}

// Update the specified user in the data access object.
func (a *User) Update(ctx context.Context, id string, formItem *schema.UserForm) error {
	user, err := a.UserDAL.Get(ctx, id)
	if err != nil {
		return err
	} else if user == nil {
		return errors.NotFound("", "User not found")
	}
	if err := a.guard().checkManageUser(ctx, id); err != nil {
		return err
	}
	if err := a.guard().checkAssignRoles(ctx, roleIDsOf(formItem.Roles)); err != nil {
		return err
	}
	if user.Username != formItem.Username {
		existsUsername, err := a.UserDAL.ExistsUsername(ctx, formItem.Username)
		if err != nil {
			return err
		} else if existsUsername {
			return errors.BadRequest("", "Username already exists")
		}
	}

	if err := formItem.FillTo(user); err != nil {
		return err
	}
	user.UpdatedAt = time.Now()

	err = a.Trans.Exec(ctx, func(ctx context.Context) error {
		if err := a.UserDAL.Update(ctx, user); err != nil {
			return err
		}

		if err := a.UserRoleDAL.DeleteByUserID(ctx, id); err != nil {
			return err
		}
		for _, userRole := range formItem.Roles {
			if userRole.ID == "" {
				userRole.ID = util.NewXID()
			}
			userRole.UserID = user.ID
			if userRole.CreatedAt.IsZero() {
				userRole.CreatedAt = time.Now()
			}
			userRole.UpdatedAt = time.Now()
			if err := a.UserRoleDAL.Create(ctx, userRole); err != nil {
				return err
			}
		}

		return nil
	})
	if err != nil {
		return err
	}
	if formItem.Password != "" {
		// A password set by an administrator signs the user out everywhere.
		if err := revokeUserTokens(ctx, a.Cache, a.Config, id); err != nil {
			return err
		}
	}
	return a.invalidateUserCache(ctx, id)
}

// Delete the specified user from the data access object.
func (a *User) Delete(ctx context.Context, id string) error {
	exists, err := a.UserDAL.Exists(ctx, id)
	if err != nil {
		return err
	} else if !exists {
		return errors.NotFound("", "User not found")
	}
	if err := a.guard().checkManageUser(ctx, id); err != nil {
		return err
	}

	err = a.Trans.Exec(ctx, func(ctx context.Context) error {
		if err := a.UserDAL.Delete(ctx, id); err != nil {
			return err
		}
		if err := a.UserRoleDAL.DeleteByUserID(ctx, id); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return err
	}
	return a.invalidateUserCache(ctx, id)
}

func (a *User) ResetPassword(ctx context.Context, id string) error {
	exists, err := a.UserDAL.Exists(ctx, id)
	if err != nil {
		return err
	} else if !exists {
		return errors.NotFound("", "User not found")
	}
	if err := a.guard().checkManageUser(ctx, id); err != nil {
		return err
	}

	if a.Config.General.DefaultLoginPwd == "" {
		return errors.BadRequest("", "Password reset is disabled because no default password is configured")
	}

	hashPass, err := hash.GeneratePassword(a.Config.General.DefaultLoginPwd)
	if err != nil {
		return errors.BadRequest("", "Failed to generate hash password: %s", err.Error())
	}

	if err := a.UserDAL.UpdatePasswordByID(ctx, id, hashPass); err != nil {
		return err
	}
	return revokeUserTokens(ctx, a.Cache, a.Config, id)
}

func (a *User) GetRoleIDs(ctx context.Context, id string) ([]string, error) {
	userRoleResult, err := a.UserRoleDAL.Query(ctx, schema.UserRoleQueryParam{
		UserID: id,
	}, schema.UserRoleQueryOptions{
		QueryOptions: util.QueryOptions{
			SelectFields: []string{"role_id"},
		},
	})
	if err != nil {
		return nil, err
	}
	return userRoleResult.Data.ToRoleIDs(), nil
}

// userCacheRedeleteDelay is how long after a change the user cache entry is
// deleted a second time. A variable so tests can shorten it.
var userCacheRedeleteDelay = 2 * time.Second

// invalidateUserCache drops the cached status and roles of a user after a
// change has committed. A request that read the old state just before the
// commit can still write it back after the first delete, so the entry is
// deleted again after a short delay; the entry also expires on its own.
func (a *User) invalidateUserCache(ctx context.Context, id string) error {
	if err := a.Cache.Delete(ctx, config.CacheNSForUser, id); err != nil {
		return err
	}
	time.AfterFunc(userCacheRedeleteDelay, func() {
		_ = a.Cache.Delete(context.Background(), config.CacheNSForUser, id)
	})
	return nil
}
