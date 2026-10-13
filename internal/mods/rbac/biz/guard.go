package biz

import (
	"context"

	"github.com/GoRustC/new-goadmin/internal/mods/rbac/dal"
	"github.com/GoRustC/new-goadmin/internal/mods/rbac/schema"
	"github.com/GoRustC/new-goadmin/pkg/errors"
	"github.com/GoRustC/new-goadmin/pkg/util"
)

// ErrPrivilegeEscalationID identifies the 403 returned when a non-root
// operator tries to grant or manage privileges they do not hold themselves.
const ErrPrivilegeEscalationID = "com.rbac.privilege-escalation"

// privilegeGuard keeps user and role management below the operator's own
// privileges. A privilege is a menu: a role's privileges are its menus, a
// user's privileges are the menus of all their roles.
//
// Without it, permission to edit users or roles was equivalent to root: an
// operator could give themselves any role, take over any account by setting
// its password, or attach any menu to their own role.
//
// Root is not restricted. Calls without an authenticated operator, such as
// startup tasks, are not restricted either; every API request passes the
// auth middleware and carries one.
type privilegeGuard struct {
	userRoles *dal.UserRole
	roleMenus *dal.RoleMenu
}

// restricted reports whether ctx carries a non-root operator, and who.
func (g privilegeGuard) restricted(ctx context.Context) (string, bool) {
	if util.FromIsRootUser(ctx) {
		return "", false
	}
	id := util.FromUserID(ctx)
	return id, id != ""
}

func (g privilegeGuard) rolesOfUser(ctx context.Context, userID string) ([]string, error) {
	result, err := g.userRoles.Query(ctx, schema.UserRoleQueryParam{UserID: userID}, schema.UserRoleQueryOptions{
		QueryOptions: util.QueryOptions{SelectFields: []string{"role_id"}},
	})
	if err != nil {
		return nil, err
	}
	return result.Data.ToRoleIDs(), nil
}

func (g privilegeGuard) menusOfRoles(ctx context.Context, roleIDs []string) (map[string]struct{}, error) {
	menus := make(map[string]struct{})
	for _, roleID := range roleIDs {
		result, err := g.roleMenus.Query(ctx, schema.RoleMenuQueryParam{RoleID: roleID}, schema.RoleMenuQueryOptions{
			QueryOptions: util.QueryOptions{SelectFields: []string{"menu_id"}},
		})
		if err != nil {
			return nil, err
		}
		for _, rm := range result.Data {
			menus[rm.MenuID] = struct{}{}
		}
	}
	return menus, nil
}

func (g privilegeGuard) operatorMenus(ctx context.Context, operatorID string) (map[string]struct{}, error) {
	roleIDs, err := g.rolesOfUser(ctx, operatorID)
	if err != nil {
		return nil, err
	}
	return g.menusOfRoles(ctx, roleIDs)
}

func within(menus, allowed map[string]struct{}) bool {
	for id := range menus {
		if _, ok := allowed[id]; !ok {
			return false
		}
	}
	return true
}

func escalation(detail string) error {
	return errors.Forbidden(ErrPrivilegeEscalationID, "%s", detail)
}

// checkAssignRoles allows assigning roleIDs only if their menus are all held
// by the operator.
func (g privilegeGuard) checkAssignRoles(ctx context.Context, roleIDs []string) error {
	operatorID, ok := g.restricted(ctx)
	if !ok || len(roleIDs) == 0 {
		return nil
	}
	allowed, err := g.operatorMenus(ctx, operatorID)
	if err != nil {
		return err
	}
	requested, err := g.menusOfRoles(ctx, roleIDs)
	if err != nil {
		return err
	}
	if !within(requested, allowed) {
		return escalation("Cannot assign roles with privileges you do not have")
	}
	return nil
}

// checkManageUser allows changing, deleting or resetting the password of
// another user whose privileges are all held by the operator.
func (g privilegeGuard) checkManageUser(ctx context.Context, targetID string) error {
	operatorID, ok := g.restricted(ctx)
	if !ok {
		return nil
	}
	if targetID == operatorID {
		return escalation("Use the current user endpoints to change your own account")
	}
	allowed, err := g.operatorMenus(ctx, operatorID)
	if err != nil {
		return err
	}
	targetRoles, err := g.rolesOfUser(ctx, targetID)
	if err != nil {
		return err
	}
	target, err := g.menusOfRoles(ctx, targetRoles)
	if err != nil {
		return err
	}
	if !within(target, allowed) {
		return escalation("Cannot manage a user with privileges you do not have")
	}
	return nil
}

// checkRoleMenus allows a role to hold menuIDs only if the operator holds
// them all. When roleID is set, the role's current menus must also be held,
// so an operator cannot change or delete a more privileged role.
func (g privilegeGuard) checkRoleMenus(ctx context.Context, roleID string, menuIDs []string) error {
	operatorID, ok := g.restricted(ctx)
	if !ok {
		return nil
	}
	allowed, err := g.operatorMenus(ctx, operatorID)
	if err != nil {
		return err
	}
	if roleID != "" {
		current, err := g.menusOfRoles(ctx, []string{roleID})
		if err != nil {
			return err
		}
		if !within(current, allowed) {
			return escalation("Cannot change a role with privileges you do not have")
		}
	}
	requested := make(map[string]struct{}, len(menuIDs))
	for _, id := range menuIDs {
		requested[id] = struct{}{}
	}
	if !within(requested, allowed) {
		return escalation("Cannot grant menus you do not have")
	}
	return nil
}

func roleIDsOf(roles schema.UserRoles) []string {
	ids := make([]string, 0, len(roles))
	for _, r := range roles {
		ids = append(ids, r.RoleID)
	}
	return ids
}

func menuIDsOf(menus schema.RoleMenus) []string {
	ids := make([]string, 0, len(menus))
	for _, m := range menus {
		ids = append(ids, m.MenuID)
	}
	return ids
}
