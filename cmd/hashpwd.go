package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/GoRustC/new-goadmin/pkg/crypto/hash"
	"github.com/urfave/cli/v2"
)

// HashPasswordCmd prints the value to use for General.Root.Password: a
// bcrypt hash of the password's MD5, which is what the frontend sends.
func HashPasswordCmd() *cli.Command {
	return &cli.Command{
		Name:      "hash-password",
		Usage:     "Print a bcrypt hash for General.Root.Password (reads the password from stdin)",
		UsageText: "ginadmin hash-password < password.txt   or   printf '%s' 'secret' | ginadmin hash-password",
		Action: func(_ *cli.Context) error {
			line, err := bufio.NewReader(os.Stdin).ReadString('\n')
			if err != nil && line == "" {
				return cli.Exit("read password from stdin: "+err.Error(), 1)
			}
			password := strings.TrimRight(line, "\r\n")
			if password == "" {
				return cli.Exit("empty password", 1)
			}
			hashed, err := hash.GeneratePassword(hash.MD5String(password))
			if err != nil {
				return cli.Exit(err.Error(), 1)
			}
			fmt.Println(hashed)
			return nil
		},
	}
}
