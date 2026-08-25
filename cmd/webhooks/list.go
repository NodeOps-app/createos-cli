package webhooks

import (
	"fmt"
	"strings"

	"github.com/pterm/pterm"
	"github.com/urfave/cli/v2"

	"github.com/NodeOps-app/createos-cli/internal/api"
	"github.com/NodeOps-app/createos-cli/internal/output"
)

func newWebhooksListCommand() *cli.Command {
	return &cli.Command{
		Name:    "list",
		Aliases: []string{"ls"},
		Usage:   "List all webhook endpoints",
		Action: func(c *cli.Context) error {
			client, ok := c.App.Metadata[api.ClientKey].(*api.APIClient)
			if !ok {
				return fmt.Errorf("you're not signed in — run 'createos login' to get started")
			}

			endpoints, err := client.ListWebhookEndpoints()
			if err != nil {
				return err
			}

			output.Render(c, endpoints, func() {
				if len(endpoints) == 0 {
					fmt.Println("You don't have any webhook endpoints yet.")
					fmt.Println()
					fmt.Println("  Create one with:")
					fmt.Println("    createos webhooks create")
					return
				}

				tableData := pterm.TableData{
					{"ID", "URL", "Events", "Status", "Failures"},
				}
				for _, ep := range endpoints {
					status := statusIcon(ep.Active)
					events := "*"
					if len(ep.Events) > 0 {
						events = strings.Join(ep.Events, ", ")
						if len(events) > 40 {
							events = events[:37] + "..."
						}
					}
					tableData = append(tableData, []string{
						ep.ID, ep.URL, events, status, fmt.Sprintf("%d", ep.FailureCount),
					})
				}
				_ = output.RenderTable(tableData) //nolint:errcheck
				fmt.Println()
			})
			return nil
		},
	}
}

func statusIcon(active bool) string {
	if active {
		return pterm.Green("active")
	}
	return pterm.Red("suspended")
}
