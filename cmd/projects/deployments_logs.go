package projects

import (
	"fmt"

	"github.com/pterm/pterm"
	"github.com/urfave/cli/v2"

	"github.com/NodeOps-app/createos-cli/internal/api"
)

func newDeploymentLogsCommand() *cli.Command {
	return &cli.Command{
		Name:      "logs",
		Usage:     "Get logs for a deployment",
		ArgsUsage: "<project-id> <deployment-id>",
		Description: "Fetches the latest logs for a running deployment.\n\n" +
			"   To find your deployment ID, run:\n" +
			"     createos projects deployments list <project-id>",
		Action: func(c *cli.Context) error {
			if c.NArg() < 2 {
				return fmt.Errorf("please provide a project ID and deployment ID\n\n  Example:\n    createos projects deployments logs <project-id> <deployment-id>")
			}

			client, ok := c.App.Metadata[api.ClientKey].(*api.APIClient)
			if !ok {
				return fmt.Errorf("you're not signed in — run 'createos login' to get started")
			}

			projectID := c.Args().Get(0)
			deploymentID := c.Args().Get(1)

			logs, err := client.GetDeploymentLogs(projectID, deploymentID)
			if err != nil {
				return err
			}

			if logs == "" {
				fmt.Println("No logs available yet. The deployment may still be starting up.")
				return nil
			}

			fmt.Println(logs)
			fmt.Println()
			pterm.Println(pterm.Gray("  Tip: To redeploy, run:"))
			pterm.Println(pterm.Gray("    createos projects deployments retrigger " + projectID + " " + deploymentID))
			return nil
		},
	}
}
