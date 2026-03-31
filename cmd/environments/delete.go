package environments

import (
	"fmt"

	"github.com/pterm/pterm"
	"github.com/urfave/cli/v2"

	"github.com/NodeOps-app/createos-cli/internal/api"
	"github.com/NodeOps-app/createos-cli/internal/terminal"
)

func newEnvironmentsDeleteCommand() *cli.Command {
	return &cli.Command{
		Name:  "delete",
		Usage: "Delete an environment",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "project", Usage: "Project ID"},
			&cli.StringFlag{Name: "environment", Usage: "Environment ID"},
			&cli.BoolFlag{Name: "force", Aliases: []string{"f"}, Usage: "Skip confirmation prompt"},
		},
		Action: func(c *cli.Context) error {
			client, ok := c.App.Metadata[api.ClientKey].(*api.APIClient)
			if !ok {
				return fmt.Errorf("you're not signed in — run 'createos login' to get started")
			}

			projectID, environmentID, err := resolveEnvironment(c, client)
			if err != nil {
				return err
			}

			if !c.Bool("force") {
				if !terminal.IsInteractive() {
					return fmt.Errorf("confirmation required — use --force to delete without a prompt")
				}

				confirm, err := pterm.DefaultInteractiveConfirm.
					WithDefaultText(fmt.Sprintf("Are you sure you want to permanently delete environment %q?", environmentID)).
					WithDefaultValue(false).
					Show()
				if err != nil {
					return fmt.Errorf("could not read confirmation: %w", err)
				}

				if !confirm {
					fmt.Println("Cancelled. Your environment was not deleted.")
					return nil
				}
			}

			if err := client.DeleteEnvironment(projectID, environmentID); err != nil {
				return err
			}

			pterm.Success.Println("Environment deletion started.")
			return nil
		},
	}
}
