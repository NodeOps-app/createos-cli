package environments

import (
	"fmt"
	"strings"

	"github.com/pterm/pterm"
	"github.com/urfave/cli/v2"

	"github.com/NodeOps-app/createos-cli/internal/api"
	"github.com/NodeOps-app/createos-cli/internal/cmdutil"
	"github.com/NodeOps-app/createos-cli/internal/output"
)

func newEnvironmentsListCommand() *cli.Command {
	return &cli.Command{
		Name:  "list",
		Usage: "List environments for a project",
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
			environments, err := client.ListEnvironments(projectID)
			if err != nil {
				return err
			}

			output.Render(c, environments, func() {
				if len(environments) == 0 {
					fmt.Println("No environments found for this project.")
					return
				}

				tableData := pterm.TableData{
					{"ID", "Name", "Branch", "Status", "URL", "Domains", "Created At"},
				}
				for _, env := range environments {
					branch := "-"
					if env.Branch != nil && *env.Branch != "" {
						branch = *env.Branch
					}
					domains := "-"
					if len(env.Extra.CustomDomains) > 0 {
						domains = strings.Join(env.Extra.CustomDomains, ", ")
					}
					tableData = append(tableData, []string{
						env.ID,
						env.DisplayName,
						branch,
						env.Status,
						env.Extra.Endpoint,
						domains,
						env.CreatedAt.Format("2006-01-02 15:04:05"),
					})
				}
				_ = pterm.DefaultTable.WithHasHeader().WithData(tableData).Render()
				fmt.Println()
			})
			return nil
		},
	}
}
