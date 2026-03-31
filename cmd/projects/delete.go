// Package projects provides project management commands.
package projects

import (
	"fmt"

	"github.com/pterm/pterm"
	"github.com/urfave/cli/v2"

	"github.com/NodeOps-app/createos-cli/internal/api"
	"github.com/NodeOps-app/createos-cli/internal/cmdutil"
	"github.com/NodeOps-app/createos-cli/internal/terminal"
)

func newDeleteCommand() *cli.Command {
	return &cli.Command{
		Name:  "delete",
		Usage: "Delete a project",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "project", Usage: "Project ID"},
			&cli.BoolFlag{
				Name:  "force",
				Usage: "Skip confirmation prompt (required in non-interactive mode)",
			},
		},
		Action: func(c *cli.Context) error {
			client, ok := c.App.Metadata[api.ClientKey].(*api.APIClient)
			if !ok {
				return fmt.Errorf("you're not signed in — run 'createos login' to get started")
			}

			id, err := cmdutil.ResolveProjectID(c.String("project"))
			if err != nil {
				return err
			}

			if !terminal.IsInteractive() && !c.Bool("force") {
				return fmt.Errorf("non-interactive mode: use --force flag to confirm deletion\n\n  Example:\n    createos projects delete %s --force", id)
			}

			if terminal.IsInteractive() && !c.Bool("force") {
				confirm, err := pterm.DefaultInteractiveConfirm.
					WithDefaultText(fmt.Sprintf("Are you sure you want to permanently delete project %q? This cannot be undone", id)).
					WithDefaultValue(false).
					Show()
				if err != nil {
					return fmt.Errorf("could not read confirmation: %w", err)
				}
				if !confirm {
					fmt.Println("Cancelled. Your project was not deleted.")
					return nil
				}
			}

			if err := client.DeleteProject(id); err != nil {
				return err
			}

			pterm.Success.Printf("Project %q has been deleted.\n", id)
			return nil
		},
	}
}
