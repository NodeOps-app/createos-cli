package templates

import (
	"fmt"

	"github.com/pterm/pterm"
	"github.com/urfave/cli/v2"

	"github.com/NodeOps-app/createos-cli/internal/api"
	"github.com/NodeOps-app/createos-cli/internal/terminal"
)

// resolveTemplate resolves a template ID from flags or interactively.
func resolveTemplate(c *cli.Context, client *api.APIClient) (string, error) {
	if id := c.String("template"); id != "" {
		return id, nil
	}
	if !terminal.IsInteractive() {
		return "", fmt.Errorf("please provide a template ID\n\n  Example:\n    createos templates %s --template <template-id>", c.Command.Name)
	}
	return pickTemplate(client)
}

func pickTemplate(client *api.APIClient) (string, error) {
	templates, err := client.ListPublishedTemplates()
	if err != nil {
		return "", err
	}
	if len(templates) == 0 {
		return "", fmt.Errorf("no templates available")
	}
	if len(templates) == 1 {
		return templates[0].ID, nil
	}

	options := make([]string, len(templates))
	for i, t := range templates {
		options[i] = t.Name
	}
	selected, err := pterm.DefaultInteractiveSelect.
		WithOptions(options).
		WithDefaultText("Select a template").
		Show()
	if err != nil {
		return "", fmt.Errorf("could not read selection: %w", err)
	}
	for i, opt := range options {
		if opt == selected {
			return templates[i].ID, nil
		}
	}
	return "", fmt.Errorf("no template selected")
}
