package auth

import (
	"fmt"

	"github.com/NodeOps-app/createos-cli/internal/config"
	"github.com/pterm/pterm"
	"github.com/urfave/cli/v2"
)

// NewLogoutCommand creates the logout command
func NewLogoutCommand() *cli.Command {
	return &cli.Command{
		Name:  "logout",
		Usage: "Sign out from CreateOS",
		Action: func(c *cli.Context) error {
			if !config.IsLoggedIn() {
				return fmt.Errorf("you're not signed in")
			}

			if err := config.DeleteToken(); err != nil {
				return fmt.Errorf("could not sign you out: %w", err)
			}

			fmt.Println("You've been signed out successfully.")
			fmt.Println()
			pterm.Println(pterm.Gray("  Tip: To sign back in, run:"))
			pterm.Println(pterm.Gray("    createos login"))
			return nil
		},
	}
}
