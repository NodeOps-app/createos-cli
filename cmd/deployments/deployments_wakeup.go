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
		ArgsUsage: "<project-id> <deployment-id>",
		Description: "Resumes a deployment that is currently sleeping.\n\n" +
			"   To find your deployment ID, run:\n" +
			"     createos projects deployments list <project-id>",
		Action: func(c *cli.Context) error {
			if c.NArg() < 2 {
				return fmt.Errorf("please provide a project ID and deployment ID\n\n  Example:\n    createos projects deployments wakeup <project-id> <deployment-id>")
			}

			client, ok := c.App.Metadata[api.ClientKey].(*api.APIClient)
			if !ok {
				return fmt.Errorf("you're not signed in — run 'createos login' to get started")
			}

			projectID := c.Args().Get(0)
			deploymentID := c.Args().Get(1)

			if err := client.WakeupDeployment(projectID, deploymentID); err != nil {
				return err
			}

			pterm.Success.Println("Your deployment is waking up.")
			fmt.Println()
			pterm.Println(pterm.Gray("  Tip: To check the status of your deployments, run:"))
			pterm.Println(pterm.Gray("    createos projects deployments list " + projectID))
			return nil
		},
	}
}
