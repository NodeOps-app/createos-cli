package environments

import (
	"fmt"

	"github.com/pterm/pterm"
	"github.com/urfave/cli/v2"

	"github.com/NodeOps-app/createos-cli/internal/api"
	"github.com/NodeOps-app/createos-cli/internal/cmdutil"
)

// resolveEnvironment resolves projectID and environmentID from flags, args, or interactively.
func resolveEnvironment(c *cli.Context, client *api.APIClient) (string, string, error) {
	args := c.Args().Slice()

	if envID := c.String("environment"); envID != "" {
		projectID, err := cmdutil.ResolveProjectID(c.String("project"))
		if err != nil {
			return "", "", err
		}
		return projectID, envID, nil
	}

	switch len(args) {
	case 0:
		projectID, err := cmdutil.ResolveProjectID(c.String("project"))
		if err != nil {
			return "", "", err
		}
		envID, err := pickEnvironment(client, projectID)
		if err != nil {
			return "", "", err
		}
		return projectID, envID, nil
	case 1:
		projectID, err := cmdutil.ResolveProjectID(c.String("project"))
		if err != nil {
			return "", "", err
		}
		return projectID, args[0], nil
	default:
		return args[0], args[1], nil
	}
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
		pterm.Println(pterm.Gray(fmt.Sprintf("  Using environment: %s", envs[0].DisplayName)))
		return envs[0].ID, nil
	}

	options := make([]string, len(envs))
	for i, e := range envs {
		options[i] = fmt.Sprintf("%s (%s)", e.DisplayName, e.Status)
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
