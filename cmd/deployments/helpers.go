package deployments

import (
	"fmt"

	"github.com/pterm/pterm"

	"github.com/NodeOps-app/createos-cli/internal/api"
	"github.com/NodeOps-app/createos-cli/internal/cmdutil"
)

// resolveDeployment resolves projectID and deploymentID from args or interactively.
// Args can be:
//   - <deployment-id>              (project resolved from .createos.json)
//   - <project-id> <deployment-id> (explicit)
//   - (none)                       → project from config, deployment from interactive select
func resolveDeployment(args []string, client *api.APIClient) (string, string, error) {
	switch len(args) {
	case 0:
		// No args — resolve project from config, then prompt for deployment
		projectID, err := cmdutil.ResolveProjectID("")
		if err != nil {
			return "", "", err
		}
		deploymentID, err := pickDeployment(client, projectID)
		if err != nil {
			return "", "", err
		}
		return projectID, deploymentID, nil
	case 1:
		// One arg — could be deployment ID (project from config)
		projectID, err := cmdutil.ResolveProjectID("")
		if err != nil {
			return "", "", err
		}
		return projectID, args[0], nil
	default:
		// Two args — project ID + deployment ID
		return args[0], args[1], nil
	}
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
		pterm.Println(pterm.Gray(fmt.Sprintf("  Using deployment: v%d (%s)", deployments[0].VersionNumber, deployments[0].Status)))
		return deployments[0].ID, nil
	}

	options := make([]string, len(deployments))
	for i, d := range deployments {
		label := fmt.Sprintf("%s  %s  %s", d.CreatedAt.Format("Jan 02 15:04"), d.Status, d.ID[:8])
		if d.Source != nil && d.Source.Commit != "" {
			commit := d.Source.Commit
			if len(commit) > 7 {
				commit = commit[:7]
			}
			msg := d.Source.CommitMessage
			if len(msg) > 50 {
				msg = msg[:50] + "…"
			}
			label = fmt.Sprintf("%s  %s  %s  %s %s", d.CreatedAt.Format("Jan 02 15:04"), d.Status, d.ID[:8], commit, msg)
		}
		options[i] = label
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
