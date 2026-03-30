package domains

import (
	"fmt"

	"github.com/pterm/pterm"
	"github.com/urfave/cli/v2"

	"github.com/NodeOps-app/createos-cli/internal/api"
)

func newDomainsAddCommand() *cli.Command {
	return &cli.Command{
		Name:      "add",
		Usage:     "Add a custom domain to a project",
		ArgsUsage: "<project-id> <domain>",
		Description: "Adds a custom domain to your project.\n\n" +
			"   After adding, point your DNS to the provided records, then run:\n" +
			"     createos domains refresh <project-id> <domain-id>",
		Action: func(c *cli.Context) error {
			if c.NArg() < 2 {
				return fmt.Errorf("please provide a project ID and domain name\n\n  Example:\n    createos domains add <project-id> myapp.com")
			}

			client, ok := c.App.Metadata[api.ClientKey].(*api.APIClient)
			if !ok {
				return fmt.Errorf("you're not signed in — run 'createos login' to get started")
			}

			projectID := c.Args().Get(0)
			name := c.Args().Get(1)

			id, err := client.AddDomain(projectID, name)
			if err != nil {
				return err
			}

			pterm.Success.Printf("Domain %q added successfully.\n", name)
			fmt.Println()
			pterm.Println(pterm.Gray("  Next step: point your DNS records to CreateOS, then verify with:"))
			pterm.Println(pterm.Gray("    createos domains refresh " + projectID + " " + id))
			return nil
		},
	}
}
