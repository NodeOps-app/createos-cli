package projects

import (
	"fmt"

	"github.com/pterm/pterm"
	"github.com/urfave/cli/v2"

	"github.com/NodeOps-app/createos-cli/internal/api"
	"github.com/NodeOps-app/createos-cli/internal/output"
)

func newListCommand() *cli.Command {
	return &cli.Command{
		Name:  "list",
		Usage: "List all projects",
		Action: func(c *cli.Context) error {
			client, ok := c.App.Metadata[api.ClientKey].(*api.APIClient)
			if !ok {
				return fmt.Errorf("you're not signed in — run 'createos login' to get started")
			}

			projects, err := client.ListProjects()
			if err != nil {
				return err
			}

			output.Render(c, projects, func() {
				if len(projects) == 0 {
					fmt.Println("You don't have any projects yet.")
					return
				}

				tableData := pterm.TableData{
					{"ID", "Name", "Status", "Type", "Created At"},
				}
				for _, p := range projects {
					tableData = append(tableData, []string{
						p.ID,
						p.DisplayName,
						p.Status,
						p.Type,
						p.CreatedAt.Format("2006-01-02 15:04:05"),
					})
				}
				_ = pterm.DefaultTable.WithHasHeader().WithData(tableData).Render()
			})
			return nil
		},
	}
}
