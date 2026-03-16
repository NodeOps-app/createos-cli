package projects

import (
	"fmt"

	"github.com/NodeOps-app/createos-cli/internal/api"
	"github.com/pterm/pterm"
	"github.com/urfave/cli/v2"
)

func newListCommand() *cli.Command {
	return &cli.Command{
		Name:  "list",
		Usage: "List all projects",
		Action: func(c *cli.Context) error {
			client, ok := c.App.Metadata[api.ClientKey].(*api.ApiClient)
			if !ok {
				return fmt.Errorf("you're not signed in — run 'createos login' to get started")
			}

			projects, err := client.ListProjects()
			if err != nil {
				return err
			}

			if len(projects) == 0 {
				fmt.Println("You don't have any projects yet.")
				return nil
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

			if err := pterm.DefaultTable.WithHasHeader().WithData(tableData).Render(); err != nil {
				return err
			}
			fmt.Println()
			pterm.Println(pterm.Gray("  Tip: To see details for a project, run:"))
			pterm.Println(pterm.Gray("    createos projects get <id>"))
			return nil
		},
	}
}
