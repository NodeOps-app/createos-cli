package webhooks

import (
	"fmt"

	"github.com/pterm/pterm"
	"github.com/urfave/cli/v2"

	"github.com/NodeOps-app/createos-cli/internal/api"
)

func newWebhooksSuspendCommand() *cli.Command {
	return &cli.Command{
		Name:  "suspend",
		Usage: "Suspend a webhook endpoint",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "endpoint", Usage: "Webhook endpoint ID"},
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

			if err := client.SuspendWebhookEndpoint(endpointID); err != nil {
				return err
			}

			pterm.Success.Println("Webhook endpoint suspended.")
			return nil
		},
	}
}
