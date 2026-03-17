package projects

import (
	"fmt"

	"github.com/pterm/pterm"
	"github.com/urfave/cli/v2"

	"github.com/NodeOps-app/createos-cli/internal/api"
)

func newDeploymentBuildLogsCommand() *cli.Command {
	return &cli.Command{
		Name:      "build-logs",
		Usage:     "Get build logs for a deployment",
		ArgsUsage: "<project-id> <deployment-id>",
		Description: "Fetches the build logs for a deployment.\n\n" +
			"   To find your deployment ID, run:\n" +
			"     createos projects deployments list <project-id>",
		Action: func(c *cli.Context) error {
			if c.NArg() < 2 {
				return fmt.Errorf("please provide a project ID and deployment ID\n\n  Example:\n    createos projects deployments build-logs <project-id> <deployment-id>")
			}

			client, ok := c.App.Metadata[api.ClientKey].(*api.APIClient)
			if !ok {
				return fmt.Errorf("you're not signed in — run 'createos login' to get started")
			}

			projectID := c.Args().Get(0)
			deploymentID := c.Args().Get(1)

			entries, err := client.GetDeploymentBuildLogs(projectID, deploymentID)
			if err != nil {
				return err
			}

			if len(entries) == 0 {
				fmt.Println("No build logs available yet.")
				return nil
			}

			for _, e := range entries {
				fmt.Println(e.Log)
			}

			fmt.Println()
			pterm.Println(pterm.Gray("  Tip: To see runtime logs, run:"))
			pterm.Println(pterm.Gray("    createos projects deployments logs " + projectID + " " + deploymentID))
			return nil
		},
	}
}
