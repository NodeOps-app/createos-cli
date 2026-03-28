package users

import (
	"fmt"

	"github.com/pterm/pterm"
	"github.com/urfave/cli/v2"

	"github.com/NodeOps-app/createos-cli/internal/terminal"
)

func newOAuthConsentsRevokeCommand() *cli.Command {
	return &cli.Command{
		Name:      "revoke",
		Usage:     "Revoke an OAuth app consent",
		ArgsUsage: "[client-id]",
		Description: "Revokes all tokens and consent granted to an OAuth client.\n\n" +
			"   To find the client ID, run:\n" +
			"     createos me oauth-consents list",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "client", Usage: "OAuth client ID"},
			&cli.BoolFlag{Name: "force", Usage: "Skip confirmation prompt (required in non-interactive mode)"},
		},
		Action: func(c *cli.Context) error {
			client, err := getClient(c)
			if err != nil {
				return err
			}

			clientID := c.String("client")
			if clientID == "" {
				clientID = c.Args().First()
			}
			if clientID == "" {
				if !terminal.IsInteractive() {
					return fmt.Errorf("please provide a client ID\n\n  Example:\n    createos me oauth-consents revoke --client <client-id>")
				}
				consents, err := client.ListOAuthConsents()
				if err != nil {
					return err
				}
				if len(consents) == 0 {
					return fmt.Errorf("you haven't granted access to any OAuth clients yet")
				}
				options := make([]string, 0, len(consents))
				for _, consent := range consents {
					if consent.ClientID != nil && *consent.ClientID != "" {
						label := *consent.ClientID
						if consent.ClientName != nil && *consent.ClientName != "" {
							label = *consent.ClientName + "  " + *consent.ClientID
						}
						options = append(options, label)
					}
				}
				selected, err := pterm.DefaultInteractiveSelect.
					WithOptions(options).
					WithDefaultText("Select a consent to revoke").
					Show()
				if err != nil {
					return fmt.Errorf("could not read selection: %w", err)
				}
				for _, consent := range consents {
					if consent.ClientID != nil {
						label := *consent.ClientID
						if consent.ClientName != nil && *consent.ClientName != "" {
							label = *consent.ClientName + "  " + *consent.ClientID
						}
						if label == selected {
							clientID = *consent.ClientID
							break
						}
					}
				}
			}

			if !c.Bool("force") {
				if !terminal.IsInteractive() {
					return fmt.Errorf("use --force to confirm revocation in non-interactive mode\n\n  Example:\n    createos me oauth-consents revoke --client %s --force", clientID)
				}
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
			}

			if err := client.RevokeOAuthConsent(clientID); err != nil {
				return err
			}

			pterm.Success.Println("OAuth consent revoked.")
			return nil
		},
	}
}
