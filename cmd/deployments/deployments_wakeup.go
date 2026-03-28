package deployments

import (
	"fmt"

	"github.com/pterm/pterm"
	"github.com/urfave/cli/v2"

	"github.com/NodeOps-app/createos-cli/internal/api"
)

func newDeploymentWakeupCommand() *cli.Command {
	return &cli.Command{
		Name:      "wakeup",
		Usage:     "Wake up a sleeping deployment",
		ArgsUsage: "[project-id] <deployment-id>",
		Description: "Resumes a deployment that is currently sleeping.\n\n" +
			"   To find your deployment ID, run:\n" +
			"     createos projects deployments list <project-id>",
		Action: func(c *cli.Context) error {
			client, ok := c.App.Metadata[api.ClientKey].(*api.APIClient)
			if !ok {
				return fmt.Errorf("you're not signed in — run 'createos login' to get started")
			}

			projectID, deploymentID, err := resolveDeployment(c.Args().Slice(), client)
			if err != nil {
				return err
			}

			if err := client.WakeupDeployment(projectID, deploymentID); err != nil {
				return err
			}

			pterm.Success.Println("Your deployment is waking up.")
			return nil
		},
	}
}
