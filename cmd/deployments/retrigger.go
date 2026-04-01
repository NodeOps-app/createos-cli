package deployments

import (
	"fmt"

	"github.com/pterm/pterm"
	"github.com/urfave/cli/v2"

	"github.com/NodeOps-app/createos-cli/internal/api"
)

func newDeploymentRetriggerCommand() *cli.Command {
	return &cli.Command{
		Name:  "retrigger",
		Usage: "Redeploy an existing deployment",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "project", Usage: "Project ID"},
			&cli.StringFlag{Name: "deployment", Usage: "Deployment ID"},
		},
		Action: func(c *cli.Context) error {
			client, ok := c.App.Metadata[api.ClientKey].(*api.APIClient)
			if !ok {
				return fmt.Errorf("you're not signed in — run 'createos login' to get started")
			}

			projectID, deploymentID, err := resolveDeployment(c, client)
			if err != nil {
				return err
			}

			if _, err := client.RetriggerDeployment(projectID, deploymentID, ""); err != nil {
				return err
			}

			pterm.Success.Println("Deployment retriggered. A new deployment is now being built.")
			return nil
		},
	}
}
