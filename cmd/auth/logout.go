package auth

import (
	"fmt"

	"github.com/urfave/cli/v2"

	"github.com/NodeOps-app/createos-cli/internal/config"
	"github.com/NodeOps-app/createos-cli/internal/telemetry"
)

// NewLogoutCommand creates the logout command
func NewLogoutCommand() *cli.Command {
	return &cli.Command{
		Name:  "logout",
		Usage: "Sign out from CreateOS",
		Action: func(_ *cli.Context) error {
			if !config.IsLoggedIn() {
				return fmt.Errorf("you're not signed in")
			}

			if err := config.DeleteToken(); err != nil {
				return fmt.Errorf("could not sign you out: %w", err)
			}
			if err := config.DeleteOAuthSession(); err != nil {
				return fmt.Errorf("could not clear your session: %w", err)
			}
			// Phase 4: clear identity binding so the next login can re-Alias
			// against the post-login user_id without inheriting stale state.
			_ = config.DeleteIdentity()

			if telemetry.Default != nil {
				telemetry.Default.Capture("auth_event", map[string]any{
					"action":  "logout",
					"success": true,
				})
			}

			fmt.Println("You've been signed out successfully.")
			return nil
		},
	}
}
