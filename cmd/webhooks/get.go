package webhooks

import (
	"fmt"
	"strings"

	"github.com/pterm/pterm"
	"github.com/urfave/cli/v2"

	"github.com/NodeOps-app/createos-cli/internal/api"
	"github.com/NodeOps-app/createos-cli/internal/output"
)

func newWebhooksGetCommand() *cli.Command {
	return &cli.Command{
		Name:      "get",
		Usage:     "Show details for a webhook endpoint",
		ArgsUsage: "<endpoint-id>",
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

			result, err := client.GetWebhookEndpoint(endpointID)
			if err != nil {
				return err
			}

			output.Render(c, result, func() {
				ep := result.Endpoint
				label := pterm.NewStyle(pterm.FgCyan)

				fmt.Printf("%s  %s\n", label.Sprint("ID:"), ep.ID)
				fmt.Printf("%s %s\n", label.Sprint("URL:"), ep.URL)
				fmt.Printf("%s %s\n", label.Sprint("Status:"), statusIcon(ep.Active))
				events := "*  (all events)"
				if len(ep.Events) > 0 {
					events = strings.Join(ep.Events, ", ")
				}
				fmt.Printf("%s %s\n", label.Sprint("Events:"), events)
				fmt.Printf("%s %d\n", label.Sprint("Failures:"), ep.FailureCount)
				fmt.Printf("%s %s\n", label.Sprint("Created:"), ep.CreatedAt.Format("2006-01-02 15:04:05 UTC"))
				fmt.Println()

				if len(result.Deliveries) == 0 {
					fmt.Println("No recent deliveries.")
					return
				}

				fmt.Println("Recent Deliveries:")
				tableData := pterm.TableData{
					{"ID", "Event", "Status", "Attempts", "Created"},
				}
				for _, d := range result.Deliveries {
					tableData = append(tableData, []string{
						d.ID,
						d.EventAction,
						deliveryStatusIcon(d.Status),
						fmt.Sprintf("%d", d.Attempts),
						d.CreatedAt.Format("2006-01-02 15:04"),
					})
				}
				_ = pterm.DefaultTable.WithHasHeader().WithData(tableData).Render() //nolint:errcheck
				fmt.Println()
			})
			return nil
		},
	}
}

func deliveryStatusIcon(status string) string {
	switch status {
	case "delivered":
		return pterm.Green("delivered")
	case "failed":
		return pterm.Red("failed")
	default:
		return pterm.Yellow("pending")
	}
}
