// Package projects provides project management commands.
package projects

import (
	"fmt"

	"github.com/pterm/pterm"
	"github.com/urfave/cli/v2"

	"github.com/NodeOps-app/createos-cli/internal/api"
)

func newDeleteCommand() *cli.Command {
	return &cli.Command{
		Name:      "delete",
		Usage:     "Delete a project",
		ArgsUsage: "<project-id>",
		Description: "Permanently deletes a project. This action cannot be undone.\n\n" +
			"   To find your project ID, run:\n" +
			"     createos projects list",
		Action: func(c *cli.Context) error {
			if c.NArg() == 0 {
				return fmt.Errorf("please provide a project ID\n\n  To see your projects and their IDs, run:\n    createos projects list")
			}

			client, ok := c.App.Metadata[api.ClientKey].(*api.APIClient)
			if !ok {
				return fmt.Errorf("you're not signed in — run 'createos login' to get started")
			}

			id := c.Args().First()

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

			if err := client.DeleteProject(id); err != nil {
				return err
			}

			pterm.Success.Printf("Project %q has been deleted.\n", id)
			fmt.Println()
			pterm.Println(pterm.Gray("  Tip: To see your remaining projects, run:"))
			pterm.Println(pterm.Gray("    createos projects list"))
			return nil
		},
	}
}
