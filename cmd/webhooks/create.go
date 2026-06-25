package webhooks

import (
	"fmt"

	atomickeys "atomicgo.dev/keyboard/keys"
	"github.com/pterm/pterm"
	"github.com/urfave/cli/v2"

	"github.com/NodeOps-app/createos-cli/internal/api"
	"github.com/NodeOps-app/createos-cli/internal/output"
	"github.com/NodeOps-app/createos-cli/internal/terminal"
)

func newWebhooksCreateCommand() *cli.Command {
	return &cli.Command{
		Name:  "create",
		Usage: "Create a new webhook endpoint",
		Description: `Create a webhook endpoint that receives event notifications via HTTP POST.

Examples:
  # Subscribe to all events:
  createos webhooks create --url https://example.com/webhook

  # Subscribe to specific events:
  createos webhooks create --url https://example.com/webhook \
    --event sandbox.create --event sandbox.destroy

  # Interactive mode (TTY only):
  createos webhooks create`,
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "url", Usage: "HTTPS URL to receive webhook events"},
			&cli.StringSliceFlag{Name: "event", Usage: "Event to subscribe to (repeatable, omit for all events)"},
		},
		Action: func(c *cli.Context) error {
			client, ok := c.App.Metadata[api.ClientKey].(*api.APIClient)
			if !ok {
				return fmt.Errorf("you're not signed in — run 'createos login' to get started")
			}

			webhookURL := c.String("url")
			events := c.StringSlice("event")

			if !terminal.IsInteractive() {
				if webhookURL == "" {
					return fmt.Errorf("please provide a URL with --url")
				}
			} else {
				if webhookURL == "" {
					var err error
					webhookURL, err = pterm.DefaultInteractiveTextInput.
						WithDefaultText("Webhook URL").
						Show()
					if err != nil {
						return fmt.Errorf("could not read URL: %w", err)
					}
				}
				if len(events) == 0 {
					actions, err := client.ListWebhookActions()
					if err != nil {
						return fmt.Errorf("could not load available events: %w", err)
					}

					fmt.Println("Select events to subscribe to (leave empty for all events):")
					selected, selErr := pterm.DefaultInteractiveMultiselect.
						WithOptions(actions).
						WithDefaultText("Events").
						WithFilter(false).
						WithKeySelect(atomickeys.Space).
						WithKeyConfirm(atomickeys.Enter).
						WithCheckmark(&pterm.Checkmark{Checked: "x", Unchecked: " "}).
						Show()
					if selErr != nil {
						return fmt.Errorf("could not read selection: %w", selErr)
					}
					events = selected
				}
			}

			if webhookURL == "" {
				return fmt.Errorf("URL cannot be empty")
			}

			req := api.CreateWebhookEndpointRequest{
				URL:    webhookURL,
				Events: events,
			}

			result, err := client.CreateWebhookEndpoint(req)
			if err != nil {
				return err
			}

			if output.IsJSON(c) {
				output.Render(c, result, func() {})
				return nil
			}

			pterm.Success.Printf("Webhook endpoint created. ID: %s\n", result.ID)
			fmt.Println()
			label := pterm.NewStyle(pterm.FgCyan)
			fmt.Printf("%s  %s\n", label.Sprint("Secret:"), result.Secret)
			fmt.Println()
			pterm.Println(pterm.Gray("  Save this secret — it won't be shown again."))
			pterm.Println(pterm.Gray("  Use it to verify webhook signatures (X-Webhook-Signature header)."))

			return nil
		},
	}
}
