package env

import (
	"fmt"

	"github.com/pterm/pterm"
	"github.com/urfave/cli/v2"

	"github.com/NodeOps-app/createos-cli/internal/api"
	"github.com/NodeOps-app/createos-cli/internal/config"
)

// resolveProjectEnv resolves the project and environment IDs from flags or .createos.json.
func resolveProjectEnv(c *cli.Context, client *api.APIClient) (string, string, error) {
	projectID := c.String("project")
	envID := c.String("environment")

	if projectID == "" || envID == "" {
		cfg, err := config.FindProjectConfig()
		if err != nil {
			return "", "", err
		}
		if cfg == nil {
			return "", "", fmt.Errorf("no project linked to this directory\n\n  Link a project first:\n    createos init\n\n  Or specify flags:\n    --project <id> --environment <id>")
		}
		if projectID == "" {
			projectID = cfg.ProjectID
		}
		if envID == "" {
			envID = cfg.EnvironmentID
		}
	}

	// If still no environment, pick the first one and inform the user
	if envID == "" {
		envs, err := client.ListEnvironments(projectID)
		if err != nil {
			return "", "", err
		}
		if len(envs) == 0 {
			return "", "", fmt.Errorf("no environments found for this project")
		}
		envID = envs[0].ID
		pterm.Println(pterm.Gray(fmt.Sprintf("  Using environment: %s (%s)", envs[0].DisplayName, envID)))
	}

	return projectID, envID, nil
}
