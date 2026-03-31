package environments

import (
	"fmt"

	"github.com/pterm/pterm"
	"github.com/urfave/cli/v2"

	"github.com/NodeOps-app/createos-cli/internal/api"
	"github.com/NodeOps-app/createos-cli/internal/cmdutil"
	"github.com/NodeOps-app/createos-cli/internal/terminal"
)

// resolveEnvironment resolves projectID and environmentID from flags or interactively.
func resolveEnvironment(c *cli.Context, client *api.APIClient) (string, string, error) {
	projectID, err := cmdutil.ResolveProjectID(c.String("project"))
	if err != nil {
		return "", "", err
	}

	if envID := c.String("environment"); envID != "" {
		return projectID, envID, nil
	}

	envID, err := pickEnvironment(client, projectID)
	if err != nil {
		return "", "", err
	}
	return projectID, envID, nil
}

func pickEnvironment(client *api.APIClient, projectID string) (string, error) {
	envs, err := client.ListEnvironments(projectID)
	if err != nil {
		return "", err
	}
	if len(envs) == 0 {
		return "", fmt.Errorf("no environments found for this project")
	}
	if len(envs) == 1 {
		return envs[0].ID, nil
	}

	options := make([]string, len(envs))
	for i, e := range envs {
		options[i] = fmt.Sprintf("%s (%s)", e.DisplayName, e.Status)
	}
	if !terminal.IsInteractive() {
		return "", fmt.Errorf("multiple environments found — use --environment <id> to specify one\n\n  To see your environments, run:\n    createos environments list")
	}
	selected, err := pterm.DefaultInteractiveSelect.
		WithOptions(options).
		WithDefaultText("Select an environment").
		Show()
	if err != nil {
		return "", fmt.Errorf("could not read selection: %w", err)
	}
	for i, opt := range options {
		if opt == selected {
			return envs[i].ID, nil
		}
	}
	return "", fmt.Errorf("no environment selected")
}
