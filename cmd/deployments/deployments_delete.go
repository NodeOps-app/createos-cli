package deployments

import (
	"fmt"

	"github.com/pterm/pterm"
	"github.com/urfave/cli/v2"

	"github.com/NodeOps-app/createos-cli/internal/api"
	"github.com/NodeOps-app/createos-cli/internal/cmdutil"
)

func newDeploymentDeleteCommand() *cli.Command {
	return &cli.Command{
		Name:      "cancel",
		Usage:     "Cancel a running deployment",
		ArgsUsage: "[project-id] <deployment-id>",
		Description: "Stops a deployment that is currently building or deploying.\n\n" +
			"   To find your deployment ID, run:\n" +
			"     createos projects deployments list <project-id>",
		Action: func(c *cli.Context) error {
			client, ok := c.App.Metadata[api.ClientKey].(*api.APIClient)
			if !ok {
				return fmt.Errorf("you're not signed in — run 'createos login' to get started")
			}

			projectID, deploymentID, err := cmdutil.ResolveProjectScopedArg(c.Args().Slice(), "a deployment ID")
			if err != nil {
				return err
			}

			confirm, err := pterm.DefaultInteractiveConfirm.
				WithDefaultText(fmt.Sprintf("Are you sure you want to cancel deployment %q?", deploymentID)).
				WithDefaultValue(false).
				Show()
			if err != nil {
				return fmt.Errorf("could not read confirmation: %w", err)
			}

			if !confirm {
				fmt.Println("Cancelled. Your deployment was not stopped.")
				return nil
			}

			if err := client.CancelDeployment(projectID, deploymentID); err != nil {
				return err
			}

			pterm.Success.Println("Deployment has been cancelled.")
			fmt.Println()
			pterm.Println(pterm.Gray("  Tip: To see your deployments, run:"))
			pterm.Println(pterm.Gray("    createos projects deployments list " + projectID))
			return nil
		},
	}
}
