package cmd

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/urfave/cli/v2"

	"github.com/GoRustC/new-goadmin/internal/bootstrap"
	"github.com/GoRustC/new-goadmin/internal/config"
)

// StartCmd returns the start command, which runs the server in the
// foreground, or in the background with --daemon.
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
			&cli.StringFlag{
				Name:    "static",
				Aliases: []string{"s"},
				Usage:   "Static files directory",
			},
			&cli.BoolFlag{
				Name:  "daemon",
				Usage: "Run as a daemon",
			},
		},
		Action: func(c *cli.Context) error {
			workDir := c.String("workdir")
			staticDir := c.String("static")
			configs := c.String("config")

			if c.Bool("daemon") {
				bin, err := filepath.Abs(os.Args[0])
				if err != nil {
					fmt.Printf("failed to get absolute path for command: %s \n", err.Error())
					return err
				}

				args := []string{"start"}
				args = append(args, "-d", workDir)
				args = append(args, "-c", configs)
				args = append(args, "-s", staticDir)
				fmt.Printf("execute command: %s %s \n", bin, strings.Join(args, " "))
				command := exec.Command(bin, args...) //nolint:gosec // Re-executes this binary.

				// Redirect stdout and stderr to log file
				stdLogFile := fmt.Sprintf("%s.log", c.App.Name)
				file, err := os.OpenFile(stdLogFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600) //nolint:gosec // Fixed name in the working directory.
				if err != nil {
					fmt.Printf("failed to open log file: %s \n", err.Error())
					return err
				}
				defer file.Close()

				command.Stdout = file
				command.Stderr = file

				err = command.Start()
				if err != nil {
					fmt.Printf("failed to start daemon thread: %s \n", err.Error())
					return err
				}

				// Don't wait for the command to finish
				// The main process will exit, allowing the daemon to run independently
				fmt.Printf("Service %s daemon thread started successfully\n", config.C.General.AppName)

				pid := command.Process.Pid
				lockFile := fmt.Sprintf("%s.lock", c.App.Name)
				//nolint:gosec // Fixed name in the working directory.
				if err := os.WriteFile(lockFile, []byte(strconv.Itoa(pid)), 0o600); err != nil {
					fmt.Printf("failed to write %s, the stop command will not find the process: %s \n", lockFile, err.Error())
				}
				fmt.Printf("service %s daemon thread started with pid %d \n", config.C.General.AppName, pid)
				os.Exit(0)
			}

			return bootstrap.Run(context.Background(), bootstrap.RunConfig{
				WorkDir:   workDir,
				Configs:   configs,
				StaticDir: staticDir,
			})
		},
	}
}
