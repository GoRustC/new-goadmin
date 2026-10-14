// Command ginadmin runs the admin API server. See README.md for configuration
// and deployment.
package main

import (
	"fmt"
	"os"

	"github.com/urfave/cli/v2"

	"github.com/GoRustC/new-goadmin/cmd"
)

// Usage: go build -ldflags "-X main.VERSION=x.x.x"
var VERSION = "v10.1.0"

// @title ginadmin
// @version v10.1.0
// @description A lightweight, flexible, elegant and full-featured RBAC scaffolding based on GIN + GORM 2.0 + Casbin 2.0 + Wire DI.
// @securityDefinitions.apikey ApiKeyAuth
// @in header
// @name Authorization
// @schemes http https
// @basePath /
func main() {
	app := cli.NewApp()
	app.Name = "ginadmin"
	app.Version = VERSION
	app.Usage = "A lightweight, flexible, elegant and full-featured RBAC scaffolding based on GIN + GORM 2.0 + Casbin 2.0 + Wire DI."
	app.Commands = []*cli.Command{
		cmd.StartCmd(),
		cmd.StopCmd(),
		cmd.VersionCmd(VERSION),
		cmd.HashPasswordCmd(),
	}
	if err := app.Run(os.Args); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
