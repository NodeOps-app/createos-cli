package projects

import (
	"fmt"

	"github.com/NodeOps-app/createos-cli/internal/api"
	"github.com/pterm/pterm"
	"github.com/urfave/cli/v2"
)

func newEnvironmentsDeleteCommand() *cli.Command {
	return &cli.Command{
		Name:      "delete",
		Usage:     "Delete an environment",
		ArgsUsage: "<project-id> <environment-id>",
		Description: "Permanently deletes an environment from your project.\n\n" +
			"   To find your environment ID, run:\n" +
			"     createos projects environments list <project-id>",
		Action: func(c *cli.Context) error {
			if c.NArg() < 2 {
				return fmt.Errorf("please provide a project ID and environment ID\n\n  Example:\n    createos projects environments delete <project-id> <environment-id>")
			}

			client, ok := c.App.Metadata[api.ClientKey].(*api.ApiClient)
			if !ok {
				return fmt.Errorf("you're not signed in — run 'createos login' to get started")
			}

			projectID := c.Args().Get(0)
			environmentID := c.Args().Get(1)

			confirm, err := pterm.DefaultInteractiveConfirm.
				WithDefaultText(fmt.Sprintf("Are you sure you want to permanently delete environment %q?", environmentID)).
				WithDefaultValue(false).
				Show()
			if err != nil {
				return fmt.Errorf("could not read confirmation: %w", err)
			}

			if !confirm {
				fmt.Println("Cancelled. Your environment was not deleted.")
				return nil
			}

			if err := client.DeleteEnvironment(projectID, environmentID); err != nil {
				return err
			}

			pterm.Success.Println("Environment deletion started.")
			fmt.Println()
			pterm.Println(pterm.Gray("  Tip: To check the environment status, run:"))
			pterm.Println(pterm.Gray("    createos projects environments list " + projectID))
			return nil
		},
	}
}
