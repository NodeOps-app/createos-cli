package deployments

import (
	"fmt"

	"github.com/pterm/pterm"
	"github.com/urfave/cli/v2"

	"github.com/NodeOps-app/createos-cli/internal/api"
	"github.com/NodeOps-app/createos-cli/internal/cmdutil"
	"github.com/NodeOps-app/createos-cli/internal/terminal"
)

// resolveDeployment resolves projectID and deploymentID from flags or interactively.
// Uses --project and --deployment flags; falls back to config and interactive select.
func resolveDeployment(c *cli.Context, client *api.APIClient) (string, string, error) {
	projectID, err := cmdutil.ResolveProjectID(c.String("project"))
	if err != nil {
		return "", "", err
	}

	if deploymentID := c.String("deployment"); deploymentID != "" {
		return projectID, deploymentID, nil
	}

	deploymentID, err := pickDeployment(client, projectID)
	if err != nil {
		return "", "", err
	}
	return projectID, deploymentID, nil
}

func pickDeployment(client *api.APIClient, projectID string) (string, error) {
	deployments, err := client.ListDeployments(projectID)
	if err != nil {
		return "", err
	}
	if len(deployments) == 0 {
		return "", fmt.Errorf("no deployments found for this project")
	}
	if len(deployments) == 1 {
		return deployments[0].ID, nil
	}

	options := make([]string, len(deployments))
	for i, d := range deployments {
		id := d.ID
		if len(id) > 8 {
			id = id[:8]
		}
		label := fmt.Sprintf("%s  %s  %s", d.CreatedAt.Format("Jan 02 15:04"), d.Status, id)
		if d.Source != nil && d.Source.Commit != "" {
			commit := d.Source.Commit
			if len(commit) > 7 {
				commit = commit[:7]
			}
			msg := d.Source.CommitMessage
			if len(msg) > 50 {
				msg = msg[:50] + "…"
			}
			label = fmt.Sprintf("%s  %s  %s  %s %s", d.CreatedAt.Format("Jan 02 15:04"), d.Status, id, commit, msg)
		}
		options[i] = label
	}
	if !terminal.IsInteractive() {
		return "", fmt.Errorf("multiple deployments found — use --deployment <id> to specify one\n\n  To see your deployments, run:\n    createos deployments list")
	}
	selected, err := pterm.DefaultInteractiveSelect.
		WithOptions(options).
		WithDefaultText("Select a deployment").
		Show()
	if err != nil {
		return "", fmt.Errorf("could not read selection: %w", err)
	}
	for i, opt := range options {
		if opt == selected {
			return deployments[i].ID, nil
		}
	}
	return "", fmt.Errorf("no deployment selected")
}
