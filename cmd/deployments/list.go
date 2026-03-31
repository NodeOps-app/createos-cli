package deployments

import (
	"fmt"

	"github.com/pterm/pterm"
	"github.com/urfave/cli/v2"

	"github.com/NodeOps-app/createos-cli/internal/api"
	"github.com/NodeOps-app/createos-cli/internal/cmdutil"
	"github.com/NodeOps-app/createos-cli/internal/output"
)

func newDeploymentsListCommand() *cli.Command {
	return &cli.Command{
		Name:  "list",
		Usage: "List deployments for a project",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "project", Usage: "Project ID"},
		},
		Action: func(c *cli.Context) error {
			client, ok := c.App.Metadata[api.ClientKey].(*api.APIClient)
			if !ok {
				return fmt.Errorf("you're not signed in — run 'createos login' to get started")
			}

			projectID, err := cmdutil.ResolveProjectID(c.String("project"))
			if err != nil {
				return err
			}
			deployments, err := client.ListDeployments(projectID)
			if err != nil {
				return err
			}

			output.Render(c, deployments, func() {
				if len(deployments) == 0 {
					fmt.Println("No deployments found for this project.")
					return
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
				_ = pterm.DefaultTable.WithHasHeader().WithData(tableData).Render()
			})
			return nil
		},
	}
}
