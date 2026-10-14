package rbac

import (
	"github.com/google/wire"

	"github.com/GoRustC/new-goadmin/internal/mods/rbac/api"
	"github.com/GoRustC/new-goadmin/internal/mods/rbac/biz"
	"github.com/GoRustC/new-goadmin/internal/mods/rbac/dal"
)

// Collection of wire providers
var Set = wire.NewSet(
	wire.Struct(new(RBAC), "*"),
	wire.Struct(new(Casbinx), "*"),
	wire.Bind(new(biz.PolicyReloader), new(*Casbinx)),
	wire.Struct(new(dal.Menu), "*"),
	wire.Struct(new(biz.Menu), "*"),
	wire.Struct(new(api.Menu), "*"),
	wire.Struct(new(dal.MenuResource), "*"),
	wire.Struct(new(dal.Role), "*"),
	wire.Struct(new(biz.Role), "*"),
	wire.Struct(new(api.Role), "*"),
	wire.Struct(new(dal.RoleMenu), "*"),
	wire.Struct(new(dal.User), "*"),
	wire.Struct(new(biz.User), "*"),
	wire.Struct(new(api.User), "*"),
	wire.Struct(new(dal.UserRole), "*"),
	wire.Struct(new(biz.Login), "*"),
	wire.Struct(new(api.Login), "*"),
	wire.Struct(new(api.Logger), "*"),
	wire.Struct(new(biz.Logger), "*"),
	wire.Struct(new(dal.Logger), "*"),
)
