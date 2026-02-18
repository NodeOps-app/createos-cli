package auth

import (
	"fmt"

	"github.com/NodeOps-app/createos-cli/internal/config"
	"github.com/urfave/cli/v2"
)

// NewLogoutCommand creates the logout command
func NewLogoutCommand() *cli.Command {
	return &cli.Command{
		Name:  "logout",
		Usage: "Sign out from CreateOS",
		Action: func(c *cli.Context) error {
			if !config.IsLoggedIn() {
				return fmt.Errorf("you are not logged in")
			}

			if err := config.DeleteToken(); err != nil {
				return fmt.Errorf("failed to logout: %w", err)
			}

			fmt.Println("Logout successful! Token removed from ~/.createos/.token")
			return nil
		},
	}
}
