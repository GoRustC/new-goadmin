package cmd

import (
	"fmt"

	"github.com/urfave/cli/v2"
)

// VersionCmd returns the version command, which prints v.
func VersionCmd(v string) *cli.Command {
	return &cli.Command{
		Name:  "version",
		Usage: "Show version",
		Action: func(_ *cli.Context) error {
			fmt.Println(v)
			return nil
		},
	}
}
