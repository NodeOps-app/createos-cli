package cronjobs

import (
	"fmt"

	"github.com/pterm/pterm"
	"github.com/urfave/cli/v2"

	"github.com/NodeOps-app/createos-cli/internal/api"
)

func newCronjobsUnsuspendCommand() *cli.Command {
	return &cli.Command{
		Name:      "unsuspend",
		Usage:     "Resume a suspended cron job",
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

			if err := client.UnsuspendCronjob(projectID, cronjobID); err != nil {
				return err
			}

			pterm.Success.Println("Cron job resumed.")
			return nil
		},
	}
}
