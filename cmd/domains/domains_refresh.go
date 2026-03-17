package domains

import (
	"fmt"

	"github.com/pterm/pterm"
	"github.com/urfave/cli/v2"

	"github.com/NodeOps-app/createos-cli/internal/api"
)

func newDomainsRefreshCommand() *cli.Command {
	return &cli.Command{
		Name:      "refresh",
		Usage:     "Re-check DNS and renew the certificate for a domain",
		ArgsUsage: "<project-id> <domain-id>",
		Description: "Triggers a DNS verification and certificate refresh for your domain.\n\n" +
			"   To find your domain ID, run:\n" +
			"     createos projects domains list <project-id>",
		Action: func(c *cli.Context) error {
			if c.NArg() < 2 {
				return fmt.Errorf("please provide a project ID and domain ID\n\n  Example:\n    createos projects domains refresh <project-id> <domain-id>")
			}

			client, ok := c.App.Metadata[api.ClientKey].(*api.APIClient)
			if !ok {
				return fmt.Errorf("you're not signed in — run 'createos login' to get started")
			}

			projectID := c.Args().Get(0)
			domainID := c.Args().Get(1)

			if err := client.RefreshDomain(projectID, domainID); err != nil {
				return err
			}

			pterm.Success.Println("Domain refresh started. This may take a few minutes.")
			fmt.Println()
			pterm.Println(pterm.Gray("  Tip: To check the domain status, run:"))
			pterm.Println(pterm.Gray("    createos projects domains list " + projectID))
			return nil
		},
	}
}
