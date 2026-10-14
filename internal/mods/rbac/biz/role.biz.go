package biz

import (
	"context"
	"time"

	"github.com/GoRustC/new-goadmin/internal/mods/rbac/dal"
	"github.com/GoRustC/new-goadmin/internal/mods/rbac/schema"
	"github.com/GoRustC/new-goadmin/pkg/errors"
	"github.com/GoRustC/new-goadmin/pkg/util"
)

// Role management for RBAC
type Role struct {
	Policy      PolicyReloader
	Trans       *util.Trans
	RoleDAL     *dal.Role
	RoleMenuDAL *dal.RoleMenu
	UserRoleDAL *dal.UserRole
}

// Query roles from the data access object based on the provided parameters and options.
func (a *Role) Query(ctx context.Context, params schema.RoleQueryParam) (*schema.RoleQueryResult, error) {
	params.Pagination = true

	var selectFields []string
	if params.ResultType == schema.RoleResultTypeSelect {
		params.Pagination = false
		selectFields = []string{"id", "name"}
	}

	result, err := a.RoleDAL.Query(ctx, params, schema.RoleQueryOptions{
		QueryOptions: util.QueryOptions{
			OrderFields: []util.OrderByParam{
				{Field: "sequence", Direction: util.DESC},
				{Field: "created_at", Direction: util.DESC},
			},
			SelectFields: selectFields,
		},
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

// Get the specified role from the data access object.
func (a *Role) Get(ctx context.Context, id string) (*schema.Role, error) {
	role, err := a.RoleDAL.Get(ctx, id)
	if err != nil {
		return nil, err
	} else if role == nil {
		return nil, errors.NotFound("", "Role not found")
	}

	roleMenuResult, err := a.RoleMenuDAL.Query(ctx, schema.RoleMenuQueryParam{
		RoleID: id,
	})
	if err != nil {
		return nil, err
	}
	role.Menus = roleMenuResult.Data

	return role, nil
}

// Create a new role in the data access object.
func (a *Role) guard() privilegeGuard {
	return privilegeGuard{userRoles: a.UserRoleDAL, roleMenus: a.RoleMenuDAL}
}

func (a *Role) Create(ctx context.Context, formItem *schema.RoleForm) (*schema.Role, error) {
	if err := a.guard().checkRoleMenus(ctx, "", menuIDsOf(formItem.Menus)); err != nil {
		return nil, err
	}
	if exists, err := a.RoleDAL.ExistsCode(ctx, formItem.Code); err != nil {
		return nil, err
	} else if exists {
		return nil, errors.Conflict("", "Role code already exists")
	}

	role := &schema.Role{
		ID:        util.NewXID(),
		CreatedAt: time.Now(),
	}
	if err := formItem.FillTo(role); err != nil {
		return nil, err
	}

	err := a.execAndReload(ctx, func(ctx context.Context) error {
		if err := a.RoleDAL.Create(ctx, role); err != nil {
			return err
		}

		for _, roleMenu := range formItem.Menus {
			roleMenu.ID = util.NewXID()
			roleMenu.RoleID = role.ID
			roleMenu.CreatedAt = time.Now()
			if err := a.RoleMenuDAL.Create(ctx, roleMenu); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	role.Menus = formItem.Menus

	return role, nil
}

// Update the specified role in the data access object.
func (a *Role) Update(ctx context.Context, id string, formItem *schema.RoleForm) error {
	role, err := a.RoleDAL.Get(ctx, id)
	if err != nil {
		return err
	} else if role == nil {
		return errors.NotFound("", "Role not found")
	}
	if err := a.guard().checkRoleMenus(ctx, id, menuIDsOf(formItem.Menus)); err != nil {
		return err
	}
	if role.Code != formItem.Code {
		if exists, err := a.RoleDAL.ExistsCode(ctx, formItem.Code); err != nil {
			return err
		} else if exists {
			return errors.Conflict("", "Role code already exists")
		}
	}

	if err := formItem.FillTo(role); err != nil {
		return err
	}
	role.UpdatedAt = time.Now()

	return a.execAndReload(ctx, func(ctx context.Context) error {
		if err := a.RoleDAL.Update(ctx, role); err != nil {
			return err
		}
		if err := a.RoleMenuDAL.DeleteByRoleID(ctx, id); err != nil {
			return err
		}
		for _, roleMenu := range formItem.Menus {
			if roleMenu.ID == "" {
				roleMenu.ID = util.NewXID()
			}
			roleMenu.RoleID = role.ID
			if roleMenu.CreatedAt.IsZero() {
				roleMenu.CreatedAt = time.Now()
			}
			roleMenu.UpdatedAt = time.Now()
			if err := a.RoleMenuDAL.Create(ctx, roleMenu); err != nil {
				return err
			}
		}
		return nil
	})
}

// Delete the specified role from the data access object.
func (a *Role) Delete(ctx context.Context, id string) error {
	exists, err := a.RoleDAL.Exists(ctx, id)
	if err != nil {
		return err
	} else if !exists {
		return errors.NotFound("", "Role not found")
	}
	if err := a.guard().checkRoleMenus(ctx, id, nil); err != nil {
		return err
	}

	return a.execAndReload(ctx, func(ctx context.Context) error {
		if err := a.RoleDAL.Delete(ctx, id); err != nil {
			return err
		}
		if err := a.RoleMenuDAL.DeleteByRoleID(ctx, id); err != nil {
			return err
		}
		if err := a.UserRoleDAL.DeleteByRoleID(ctx, id); err != nil {
			return err
		}

		return nil
	})
}

// execAndReload runs fn in a transaction and, once it has committed,
// rebuilds the authorization policy so the change applies immediately.
// Reloading inside the transaction would read the pre-commit data.
func (a *Role) execAndReload(ctx context.Context, fn util.TransFunc) error {
	if err := a.Trans.Exec(ctx, fn); err != nil {
		return err
	}
	return a.Policy.Reload(ctx)
}
