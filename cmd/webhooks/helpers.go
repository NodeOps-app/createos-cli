package webhooks

import (
	"fmt"

	"github.com/pterm/pterm"
	"github.com/urfave/cli/v2"

	"github.com/NodeOps-app/createos-cli/internal/api"
	"github.com/NodeOps-app/createos-cli/internal/terminal"
)

// resolveEndpoint resolves a webhook endpoint ID from the --endpoint flag,
// the first positional arg, or an interactive picker (TTY only).
func resolveEndpoint(c *cli.Context, client *api.APIClient) (string, error) {
	if id := c.String("endpoint"); id != "" {
		return id, nil
	}
	if c.Args().Len() > 0 {
		return c.Args().First(), nil
	}

	if !terminal.IsInteractive() {
		return "", fmt.Errorf(
			"please provide a webhook endpoint ID\n\n  Example:\n    createos webhooks %s --endpoint <endpoint-id>\n\n  To see your endpoints, run:\n    createos webhooks list",
			c.Command.Name,
		)
	}

	return pickEndpoint(client)
}

// pickEndpoint interactively selects a webhook endpoint from the user's list.
func pickEndpoint(client *api.APIClient) (string, error) {
	endpoints, err := client.ListWebhookEndpoints()
	if err != nil {
		return "", err
	}
	if len(endpoints) == 0 {
		return "", fmt.Errorf("you don't have any webhook endpoints yet\n\n  Create one with:\n    createos webhooks create")
	}
	if len(endpoints) == 1 {
		return endpoints[0].ID, nil
	}

	options := make([]string, len(endpoints))
	for i, ep := range endpoints {
		status := "active"
		if !ep.Active {
			status = "suspended"
		}
		options[i] = fmt.Sprintf("%s  %s  (%s)", ep.URL, ep.ID[:8], status)
	}

	selected, err := pterm.DefaultInteractiveSelect.
		WithOptions(options).
		WithDefaultText("Select a webhook endpoint").
		Show()
	if err != nil {
		return "", fmt.Errorf("could not read selection: %w", err)
	}
	for i, opt := range options {
		if opt == selected {
			return endpoints[i].ID, nil
		}
	}
	return "", fmt.Errorf("no endpoint selected")
}
