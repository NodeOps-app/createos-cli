package cronjobs

import (
	"fmt"

	"github.com/pterm/pterm"
	"github.com/urfave/cli/v2"

	"github.com/NodeOps-app/createos-cli/internal/api"
	"github.com/NodeOps-app/createos-cli/internal/cmdutil"
	"github.com/NodeOps-app/createos-cli/internal/terminal"
)

// resolveCronjob resolves a project ID and cron job ID from flags, positional
// args, or interactively (TTY only).
func resolveCronjob(c *cli.Context, client *api.APIClient) (string, string, error) {
	if cronjobID := c.String("cronjob"); cronjobID != "" {
		projectID, err := cmdutil.ResolveProjectID(c.String("project"))
		if err != nil {
			return "", "", err
		}
		return projectID, cronjobID, nil
	}

	args := c.Args().Slice()
	switch len(args) {
	case 0:
		// No args — resolve project then pick interactively.
		projectID, err := cmdutil.ResolveProjectID(c.String("project"))
		if err != nil {
			return "", "", err
		}
		if !terminal.IsInteractive() {
			return "", "", fmt.Errorf(
				"please provide a cronjob ID\n\n  Example:\n    createos cronjobs %s --cronjob <cronjob-id>",
				c.Command.Name,
			)
		}
		cronjobID, err := pickCronjob(client, projectID)
		if err != nil {
			return "", "", err
		}
		return projectID, cronjobID, nil
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

// pickCronjob interactively selects a cron job from the project's list.
func pickCronjob(client *api.APIClient, projectID string) (string, error) {
	cronjobs, err := client.ListCronjobs(projectID)
	if err != nil {
		return "", err
	}
	if len(cronjobs) == 0 {
		return "", fmt.Errorf("no cron jobs found for this project")
	}
	if len(cronjobs) == 1 {
		return cronjobs[0].ID, nil
	}

	options := make([]string, len(cronjobs))
	for i, cj := range cronjobs {
		options[i] = fmt.Sprintf("%s  %s  (%s)", cj.Name, cj.Schedule, cj.Status)
	}

	selected, err := pterm.DefaultInteractiveSelect.
		WithOptions(options).
		WithDefaultText("Select a cron job").
		Show()
	if err != nil {
		return "", fmt.Errorf("could not read selection: %w", err)
	}
	for i, opt := range options {
		if opt == selected {
			return cronjobs[i].ID, nil
		}
	}
	return "", fmt.Errorf("no cron job selected")
}
