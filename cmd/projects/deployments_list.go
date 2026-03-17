package projects

import (
	"fmt"

	"github.com/pterm/pterm"
	"github.com/urfave/cli/v2"

	"github.com/NodeOps-app/createos-cli/internal/api"
)

func newDeploymentsListCommand() *cli.Command {
	return &cli.Command{
		Name:      "list",
		Usage:     "List deployments for a project",
		ArgsUsage: "<project-id>",
		Action: func(c *cli.Context) error {
			if c.NArg() == 0 {
				return fmt.Errorf("please provide a project ID\n\n  To see your projects and their IDs, run:\n    createos projects list")
			}

			client, ok := c.App.Metadata[api.ClientKey].(*api.APIClient)
			if !ok {
				return fmt.Errorf("you're not signed in — run 'createos login' to get started")
			}

			projectID := c.Args().First()
			deployments, err := client.ListDeployments(projectID)
			if err != nil {
				return err
			}

			if len(deployments) == 0 {
				fmt.Println("No deployments found for this project.")
				return nil
			}

			tableData := pterm.TableData{
				{"ID", "Version", "Status", "URL", "Created At"},
			}
			for _, d := range deployments {
				tableData = append(tableData, []string{
					d.ID,
					fmt.Sprintf("v%d", d.VersionNumber),
					d.Status,
					d.Extra.Endpoint,
					d.CreatedAt.Format("2006-01-02 15:04:05"),
				})
			}

			if err := pterm.DefaultTable.WithHasHeader().WithData(tableData).Render(); err != nil {
				return err
			}
			fmt.Println()
			pterm.Println(pterm.Gray("  Tip: To see logs for a deployment, run:"))
			pterm.Println(pterm.Gray("    createos projects deployments logs " + projectID + " <deployment-id>"))
			return nil
		},
	}
}
