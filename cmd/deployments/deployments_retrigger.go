package deployments

import (
	"fmt"

	"github.com/pterm/pterm"
	"github.com/urfave/cli/v2"

	"github.com/NodeOps-app/createos-cli/internal/api"
	"github.com/NodeOps-app/createos-cli/internal/cmdutil"
)

func newDeploymentRetriggerCommand() *cli.Command {
	return &cli.Command{
		Name:      "retrigger",
		Usage:     "Redeploy an existing deployment",
		ArgsUsage: "[project-id] <deployment-id>",
		Description: "Creates a new deployment based on an existing one.\n\n" +
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

			if err := client.RetriggerDeployment(projectID, deploymentID); err != nil {
				return err
			}

			pterm.Success.Println("Deployment retriggered. A new deployment is now being built.")
			fmt.Println()
			pterm.Println(pterm.Gray("  Tip: To check the status of your deployments, run:"))
			pterm.Println(pterm.Gray("    createos projects deployments list " + projectID))
			return nil
		},
	}
}
