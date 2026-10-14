package cmd

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"syscall"

	"github.com/urfave/cli/v2"
)

// StopCmd returns the stop command, which signals the process recorded in
// the lock file by a daemon start and removes the file.
func StopCmd() *cli.Command {
	return &cli.Command{
		Name:  "stop",
		Usage: "stop server",
		Action: func(c *cli.Context) error {
			appName := c.App.Name
			lockFile := fmt.Sprintf("%s.lock", appName)
			content, err := os.ReadFile(lockFile) //nolint:gosec // Fixed name in the working directory.
			if err != nil {
				return err
			}
			pid, err := strconv.Atoi(strings.TrimSpace(string(content)))
			if err != nil || pid <= 1 {
				return fmt.Errorf("%s does not contain a valid process id: %q", lockFile, content)
			}

			// Ask the server to shut down gracefully; keep the lock file if
			// the signal could not be delivered.
			proc, err := os.FindProcess(pid)
			if err != nil {
				return err
			}
			if err := proc.Signal(syscall.SIGTERM); err != nil {
				return fmt.Errorf("signal process %d: %w", pid, err)
			}

			err = os.Remove(lockFile)
			if err != nil {
				return fmt.Errorf("can't remove %s.lock. %s", appName, err.Error())
			}

			fmt.Printf("service %s stopped \n", appName)
			return nil
		},
	}
}
