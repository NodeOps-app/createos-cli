package webhooks

import (
	"fmt"

	"github.com/pterm/pterm"
	"github.com/urfave/cli/v2"

	"github.com/NodeOps-app/createos-cli/internal/api"
	"github.com/NodeOps-app/createos-cli/internal/terminal"
)

func newWebhooksDeleteCommand() *cli.Command {
	return &cli.Command{
		Name:  "delete",
		Usage: "Delete a webhook endpoint",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "endpoint", Usage: "Webhook endpoint ID"},
			&cli.BoolFlag{Name: "force", Aliases: []string{"f"}, Usage: "Skip confirmation prompt"},
		},
		Action: func(c *cli.Context) error {
			client, ok := c.App.Metadata[api.ClientKey].(*api.APIClient)
			if !ok {
				return fmt.Errorf("you're not signed in — run 'createos login' to get started")
			}

			endpointID, err := resolveEndpoint(c, client)
			if err != nil {
				return err
			}

			if !c.Bool("force") {
				if !terminal.IsInteractive() {
					return fmt.Errorf("confirmation required — use --force to delete without a prompt")
				}

				confirm, confirmErr := pterm.DefaultInteractiveConfirm.
					WithDefaultText(fmt.Sprintf("Are you sure you want to delete webhook endpoint %q?", endpointID)).
					WithDefaultValue(false).
					Show()
				if confirmErr != nil {
					return fmt.Errorf("could not read confirmation: %w", confirmErr)
				}
				if !confirm {
					fmt.Println("Cancelled. Your webhook endpoint was not deleted.")
					return nil
				}
			}

			if err := client.DeleteWebhookEndpoint(endpointID); err != nil {
				return err
			}

			pterm.Success.Println("Webhook endpoint deleted.")
			return nil
		},
	}
}
