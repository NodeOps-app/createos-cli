package cronjobs

import (
	"fmt"

	"github.com/pterm/pterm"
	"github.com/urfave/cli/v2"

	"github.com/NodeOps-app/createos-cli/internal/api"
)

func newCronjobsGetCommand() *cli.Command {
	return &cli.Command{
		Name:      "get",
		Usage:     "Show details for a cron job",
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

			cj, err := client.GetCronjob(projectID, cronjobID)
			if err != nil {
				return err
			}

			label := pterm.NewStyle(pterm.FgCyan)

			label.Print("ID:            ")
			fmt.Println(cj.ID)
			label.Print("Name:          ")
			fmt.Println(cj.Name)
			label.Print("Schedule:      ")
			fmt.Println(cj.Schedule)
			label.Print("Type:          ")
			fmt.Println(cj.Type)
			label.Print("Status:        ")
			fmt.Println(cj.Status)
			label.Print("Environment:   ")
			fmt.Println(cj.EnvironmentID)
			label.Print("Project:       ")
			fmt.Println(cj.ProjectID)

			if cj.SuspendedAt != nil {
				label.Print("Suspended At:  ")
				fmt.Println(cj.SuspendedAt.Format("2006-01-02 15:04:05"))
			}
			if cj.SuspendText != nil && *cj.SuspendText != "" {
				label.Print("Suspend Text:  ")
				fmt.Println(*cj.SuspendText)
			}

			label.Print("Created At:    ")
			fmt.Println(cj.CreatedAt.Format("2006-01-02 15:04:05"))
			label.Print("Updated At:    ")
			fmt.Println(cj.UpdatedAt.Format("2006-01-02 15:04:05"))

			return nil
		},
	}
}
