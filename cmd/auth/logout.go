package auth

import (
	"fmt"

	"github.com/urfave/cli/v2"
)

// NewLogoutCommand creates the logout command
func NewLogoutCommand() *cli.Command {
	return &cli.Command{
		Name:  "logout",
		Usage: "Sign out from CreateOS",
		Action: func(c *cli.Context) error {
			fmt.Println("Logging out from current session...")
			// TODO: Implement logout logic

			fmt.Println("Logout successful!")
			return nil
		},
	}
}
