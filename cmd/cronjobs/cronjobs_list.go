package cronjobs

import (
	"fmt"

	"github.com/pterm/pterm"
	"github.com/urfave/cli/v2"

	"github.com/NodeOps-app/createos-cli/internal/api"
	"github.com/NodeOps-app/createos-cli/internal/cmdutil"
)

func newCronjobsListCommand() *cli.Command {
	return &cli.Command{
		Name:      "list",
		Usage:     "List cron jobs for a project",
		ArgsUsage: "[project-id]",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "project", Usage: "Project ID"},
		},
		Action: func(c *cli.Context) error {
			client, ok := c.App.Metadata[api.ClientKey].(*api.APIClient)
			if !ok {
				return fmt.Errorf("you're not signed in — run 'createos login' to get started")
			}

			id := c.String("project")
			if id == "" {
				id = c.Args().First()
			}
			projectID, err := cmdutil.ResolveProjectID(id)
			if err != nil {
				return err
			}

			cronjobs, err := client.ListCronjobs(projectID)
			if err != nil {
				return err
			}

			if len(cronjobs) == 0 {
				fmt.Println("You don't have any cron jobs for this project yet.")
				return nil
			}

			tableData := pterm.TableData{
				{"ID", "Name", "Schedule", "Type", "Status", "Created At"},
			}
			for _, cj := range cronjobs {
				tableData = append(tableData, []string{
					cj.ID,
					cj.Name,
					cj.Schedule,
					cj.Type,
					cj.Status,
					cj.CreatedAt.Format("2006-01-02 15:04:05"),
				})
			}

			if err := pterm.DefaultTable.WithHasHeader().WithData(tableData).Render(); err != nil {
				return err
			}
			fmt.Println()
			return nil
		},
	}
}
