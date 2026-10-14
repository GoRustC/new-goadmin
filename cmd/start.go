package cmd

import (
	"context"

	"github.com/urfave/cli/v2"

	"github.com/GoRustC/new-goadmin/internal/bootstrap"
)

// StartCmd returns the start command, which runs the server in the
// foreground until it receives SIGINT or SIGTERM. In production, run it
// under systemd (see deploy/ginadmin.service).
func StartCmd() *cli.Command {
	return &cli.Command{
		Name:  "start",
		Usage: "Start server",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:        "workdir",
				Aliases:     []string{"d"},
				Usage:       "Working directory",
				DefaultText: "configs",
				Value:       "configs",
			},
			&cli.StringFlag{
				Name:     "config",
				Aliases:  []string{"c"},
				Usage:    "Runtime configuration files or directory, relative to workdir, comma separated: dev for local development, prod for production",
				Required: true,
			},
		},
		Action: func(c *cli.Context) error {
			return bootstrap.Run(context.Background(), bootstrap.RunConfig{
				WorkDir: c.String("workdir"),
				Configs: c.String("config"),
			})
		},
	}
}
