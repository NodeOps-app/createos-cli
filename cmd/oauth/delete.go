package oauth

import (
	"fmt"

	"github.com/pterm/pterm"
	"github.com/urfave/cli/v2"

	"github.com/NodeOps-app/createos-cli/internal/terminal"
)

func newDeleteCommand() *cli.Command {
	return &cli.Command{
		Name:  "delete",
		Usage: "Delete an OAuth client",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "client", Usage: "OAuth client ID"},
			&cli.BoolFlag{Name: "force", Usage: "Skip confirmation prompt (required in non-interactive mode)"},
		},
		Action: func(c *cli.Context) error {
			apiClient, err := getClient(c)
			if err != nil {
				return err
			}

			clientID, err := resolveOAuthClientID(c, apiClient)
			if err != nil {
				return err
			}

			if !c.Bool("force") {
				if !terminal.IsInteractive() {
					return fmt.Errorf("use --force to confirm deletion in non-interactive mode\n\n  Example:\n    createos oauth-clients delete --client %s --force", clientID)
				}
				confirm, err := pterm.DefaultInteractiveConfirm.
					WithDefaultText(fmt.Sprintf("Permanently delete OAuth client %q?", clientID)).
					WithDefaultValue(false).
					Show()
				if err != nil {
					return fmt.Errorf("could not read confirmation: %w", err)
				}
				if !confirm {
					fmt.Println("Cancelled. Your OAuth client was not deleted.")
					return nil
				}
			}

			if err := apiClient.DeleteOAuthClient(clientID); err != nil {
				return err
			}

			pterm.Success.Println("OAuth client deleted.")
			return nil
		},
	}
}
