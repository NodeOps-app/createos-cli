package deployments

import (
	"fmt"

	"github.com/pterm/pterm"
	"github.com/urfave/cli/v2"

	"github.com/NodeOps-app/createos-cli/internal/api"
	"github.com/NodeOps-app/createos-cli/internal/terminal"
)

// promotableStatuses are deployment statuses whose artifact has been pushed.
var promotableStatuses = []string{"deployed", "deploying", "crashing", "sleeping", "terminating"}

func newDeploymentPromoteCommand() *cli.Command {
	return &cli.Command{
		Name:  "promote",
		Usage: "Promote a deployment to an environment",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "project", Usage: "Project ID"},
			&cli.StringFlag{Name: "deployment", Usage: "Deployment ID"},
			&cli.StringFlag{Name: "environment", Usage: "Environment ID"},
		},
		Action: func(c *cli.Context) error {
			client, ok := c.App.Metadata[api.ClientKey].(*api.APIClient)
			if !ok {
				return fmt.Errorf("you're not signed in — run 'createos login' to get started")
			}

			projectID, deploymentID, err := resolveDeployment(c, client, promotableStatuses)
			if err != nil {
				return err
			}

			environmentID := c.String("environment")
			if environmentID == "" {
				environmentID, err = pickEnvironment(client, projectID)
				if err != nil {
					return err
				}
			}

			if err := client.PromoteDeployment(projectID, environmentID, deploymentID); err != nil {
				return err
			}

			pterm.Success.Println("Deployment promoted to environment.")
			return nil
		},
	}
}

func pickEnvironment(client *api.APIClient, projectID string) (string, error) {
	envs, err := client.ListEnvironments(projectID)
	if err != nil {
		return "", err
	}
	if len(envs) == 0 {
		return "", fmt.Errorf("no environments found for this project\n\n  Create one first with:\n    createos environments create")
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
		WithDefaultText("Select an environment to promote to").
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
