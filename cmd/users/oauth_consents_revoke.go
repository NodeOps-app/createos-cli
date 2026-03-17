package users

import (
	"fmt"

	"github.com/pterm/pterm"
	"github.com/urfave/cli/v2"
)

func newOAuthConsentsRevokeCommand() *cli.Command {
	return &cli.Command{
		Name:      "revoke",
		Usage:     "Revoke an OAuth app consent",
		ArgsUsage: "<client-id>",
		Description: "Revokes all tokens and consent granted to an OAuth client.\n\n" +
			"   To find the client ID, run:\n" +
			"     createos users oauth-consents list",
		Action: func(c *cli.Context) error {
			if c.NArg() == 0 {
				return fmt.Errorf("please provide a client ID\n\n  To see your OAuth consents and their client IDs, run:\n    createos users oauth-consents list")
			}

			client, err := getClient(c)
			if err != nil {
				return err
			}

			clientID := c.Args().First()
			confirm, err := pterm.DefaultInteractiveConfirm.
				WithDefaultText(fmt.Sprintf("Are you sure you want to revoke consent for client %q?", clientID)).
				WithDefaultValue(false).
				Show()
			if err != nil {
				return fmt.Errorf("could not read confirmation: %w", err)
			}

			if !confirm {
				fmt.Println("Cancelled. The OAuth consent was not revoked.")
				return nil
			}

			if err := client.RevokeOAuthConsent(clientID); err != nil {
				return err
			}

			pterm.Success.Println("OAuth consent revoked successfully.")
			fmt.Println()
			pterm.Println(pterm.Gray("  Hint: To review your remaining consents, run:"))
			pterm.Println(pterm.Gray("    createos users oauth-consents list"))
			return nil
		},
	}
}
