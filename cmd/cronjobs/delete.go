package cronjobs

import (
	"fmt"

	"github.com/pterm/pterm"
	"github.com/urfave/cli/v2"

	"github.com/NodeOps-app/createos-cli/internal/api"
	"github.com/NodeOps-app/createos-cli/internal/terminal"
)

func newCronjobsDeleteCommand() *cli.Command {
	return &cli.Command{
		Name:  "delete",
		Usage: "Delete a cron job",
		Description: `Permanently delete a cron job. This action cannot be undone.

Examples:
  createos cronjobs delete --cronjob <cronjob-id>
  createos cronjobs delete --cronjob <cronjob-id> --force`,
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "project", Usage: "Project ID"},
			&cli.StringFlag{Name: "cronjob", Usage: "Cron job ID"},
			&cli.BoolFlag{Name: "force", Usage: "Skip confirmation prompt (for non-interactive use)"},
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

			if !c.Bool("force") {
				if !terminal.IsInteractive() {
					return fmt.Errorf(
						"this is a destructive operation — pass --force to confirm deletion in non-interactive mode",
					)
				}
				confirm, err := pterm.DefaultInteractiveConfirm.
					WithDefaultText(fmt.Sprintf("Are you sure you want to permanently delete cron job %q?", cronjobID)).
					WithDefaultValue(false).
					Show()
				if err != nil {
					return fmt.Errorf("could not read confirmation: %w", err)
				}
				if !confirm {
					fmt.Println("Cancelled. Your cron job was not deleted.")
					return nil
				}
			}

			if err := client.DeleteCronjob(projectID, cronjobID); err != nil {
				return err
			}

			pterm.Success.Println("Cron job deleted.")
			return nil
		},
	}
}
