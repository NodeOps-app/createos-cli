package domains

import (
	"fmt"

	"github.com/pterm/pterm"
	"github.com/urfave/cli/v2"

	"github.com/NodeOps-app/createos-cli/internal/api"
)

func newDomainsDeleteCommand() *cli.Command {
	return &cli.Command{
		Name:      "delete",
		Usage:     "Remove a custom domain from a project",
		ArgsUsage: "<project-id> <domain-id>",
		Description: "Permanently removes a custom domain from your project.\n\n" +
			"   To find your domain ID, run:\n" +
			"     createos projects domains list <project-id>",
		Action: func(c *cli.Context) error {
			if c.NArg() < 2 {
				return fmt.Errorf("please provide a project ID and domain ID\n\n  Example:\n    createos projects domains delete <project-id> <domain-id>")
			}

			client, ok := c.App.Metadata[api.ClientKey].(*api.APIClient)
			if !ok {
				return fmt.Errorf("you're not signed in — run 'createos login' to get started")
			}

			projectID := c.Args().Get(0)
			domainID := c.Args().Get(1)

			confirm, err := pterm.DefaultInteractiveConfirm.
				WithDefaultText(fmt.Sprintf("Are you sure you want to remove domain %q from this project?", domainID)).
				WithDefaultValue(false).
				Show()
			if err != nil {
				return fmt.Errorf("could not read confirmation: %w", err)
			}

			if !confirm {
				fmt.Println("Cancelled. Your domain was not removed.")
				return nil
			}

			if err := client.DeleteDomain(projectID, domainID); err != nil {
				return err
			}

			pterm.Success.Println("Domain is being removed.")
			fmt.Println()
			pterm.Println(pterm.Gray("  Tip: To see your remaining domains, run:"))
			pterm.Println(pterm.Gray("    createos projects domains list " + projectID))
			return nil
		},
	}
}
