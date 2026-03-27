package domains

import (
	"fmt"

	"github.com/pterm/pterm"
	"github.com/urfave/cli/v2"

	"github.com/NodeOps-app/createos-cli/internal/api"
	"github.com/NodeOps-app/createos-cli/internal/cmdutil"
)

func newDomainsListCommand() *cli.Command {
	return &cli.Command{
		Name:      "list",
		Usage:     "List all custom domains for a project",
		ArgsUsage: "[project-id]",
		Action: func(c *cli.Context) error {
			client, ok := c.App.Metadata[api.ClientKey].(*api.APIClient)
			if !ok {
				return fmt.Errorf("you're not signed in — run 'createos login' to get started")
			}

			projectID, err := cmdutil.ResolveProjectID(c.Args().First())
			if err != nil {
				return err
			}
			domains, err := client.ListDomains(projectID)
			if err != nil {
				return err
			}

			if len(domains) == 0 {
				fmt.Println("No custom domains added yet.")
				fmt.Println()
				pterm.Println(pterm.Gray("  Tip: To add a domain, run:"))
				pterm.Println(pterm.Gray("    createos domains add " + projectID + " <your-domain.com>"))
				return nil
			}

			tableData := pterm.TableData{
				{"ID", "Domain", "Status", "Message"},
			}
			for _, d := range domains {
				icon := domainIcon(d.Status)
				msg := ""
				if d.Message != nil {
					msg = *d.Message
				}
				tableData = append(tableData, []string{d.ID, d.Name, icon + " " + d.Status, msg})
			}

			if err := pterm.DefaultTable.WithHasHeader().WithData(tableData).Render(); err != nil {
				return err
			}
			fmt.Println()
			pterm.Println(pterm.Gray("  Tip: To add a new domain, run:"))
			pterm.Println(pterm.Gray("    createos domains add " + projectID + " <your-domain.com>"))
			return nil
		},
	}
}

func domainIcon(status string) string {
	switch status {
	case "verified", "active":
		return pterm.Green("✓")
	case "pending":
		return pterm.Yellow("⏳")
	default:
		return pterm.Red("✗")
	}
}
