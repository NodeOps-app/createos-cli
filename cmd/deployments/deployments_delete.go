package deployments

import (
	"fmt"

	"github.com/pterm/pterm"
	"github.com/urfave/cli/v2"

	"github.com/NodeOps-app/createos-cli/internal/api"
)

func newDeploymentDeleteCommand() *cli.Command {
	return &cli.Command{
		Name:      "cancel",
		Usage:     "Cancel a running deployment",
		ArgsUsage: "[project-id] <deployment-id>",
		Description: "Stops a deployment that is currently building or deploying.\n\n" +
			"   To find your deployment ID, run:\n" +
			"     createos deployments list <project-id>",
		Action: func(c *cli.Context) error {
			client, ok := c.App.Metadata[api.ClientKey].(*api.APIClient)
			if !ok {
				return fmt.Errorf("you're not signed in — run 'createos login' to get started")
			}

			projectID, deploymentID, err := resolveDeployment(c.Args().Slice(), client)
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
				fmt.Println("Aborted. Your deployment was not cancelled.")
				return nil
			}

			if err := client.CancelDeployment(projectID, deploymentID); err != nil {
				return err
			}

			pterm.Success.Println("Deployment has been cancelled.")
			return nil
		},
	}
}
