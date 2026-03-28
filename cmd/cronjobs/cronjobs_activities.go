package cronjobs

import (
	"fmt"

	"github.com/pterm/pterm"
	"github.com/urfave/cli/v2"

	"github.com/NodeOps-app/createos-cli/internal/api"
)

func newCronjobsActivitiesCommand() *cli.Command {
	return &cli.Command{
		Name:      "activities",
		Usage:     "Show recent execution history for a cron job",
		ArgsUsage: "[project-id] [cronjob-id]",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "project", Usage: "Project ID"},
			&cli.StringFlag{Name: "cronjob", Usage: "Cron job ID"},
		},
		Action: func(c *cli.Context) error {
			client, ok := c.App.Metadata[api.ClientKey].(*api.APIClient)
			if !ok {
				return fmt.Errorf("you're not signed in — run 'createos login' to get started")
			}

			projectID, cronjobID, err := resolveCronjob(c, client)
			if err != nil {
				return err
			}

			activities, err := client.ListCronjobActivities(projectID, cronjobID)
			if err != nil {
				return err
			}

			if len(activities) == 0 {
				fmt.Println("No execution history found for this cron job yet.")
				return nil
			}

			tableData := pterm.TableData{
				{"ID", "Success", "Status Code", "Scheduled At", "Log"},
			}
			for _, a := range activities {
				success := "-"
				if a.Success != nil {
					if *a.Success {
						success = "yes"
					} else {
						success = "no"
					}
				}
				statusCode := "-"
				if a.StatusCode != nil {
					statusCode = fmt.Sprintf("%d", *a.StatusCode)
				}
				log := "-"
				if a.Log != nil && *a.Log != "" {
					log = *a.Log
					if len(log) > 80 {
						log = log[:77] + "..."
					}
				}
				tableData = append(tableData, []string{
					a.ID,
					success,
					statusCode,
					a.ScheduledAt.Format("2006-01-02 15:04:05"),
					log,
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
