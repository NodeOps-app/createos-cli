package cronjobs

import (
	"fmt"

	"github.com/pterm/pterm"
	"github.com/urfave/cli/v2"

	"github.com/NodeOps-app/createos-cli/internal/api"
)

func newCronjobsSuspendCommand() *cli.Command {
	return &cli.Command{
		Name:      "suspend",
		Usage:     "Pause a cron job so it stops running on schedule",
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

			if err := client.SuspendCronjob(projectID, cronjobID); err != nil {
				return err
			}

			pterm.Success.Println("Cron job suspended.")
			return nil
		},
	}
}
