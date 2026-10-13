//go:build wireinject
// +build wireinject

package wirex

// The build tag makes sure the stub is not built in the final build.

import (
	"context"

	"github.com/google/wire"

	"github.com/GoRustC/new-goadmin/internal/mods"
	"github.com/GoRustC/new-goadmin/pkg/util"
)

func BuildInjector(ctx context.Context) (*Injector, func(), error) {
	wire.Build(
		ProvideConfig,
		InitCacher,
		InitDB,
		InitAuth,
		wire.NewSet(wire.Struct(new(util.Trans), "*")),
		wire.NewSet(wire.Struct(new(Injector), "*")),
		mods.Set,
	) // end
	return new(Injector), nil, nil
}
