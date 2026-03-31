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
				return nil
			}

			// Build env ID → name map for display
			envName := map[string]string{}
			if envs, err := client.ListEnvironments(projectID); err == nil {
				for _, e := range envs {
					envName[e.ID] = e.DisplayName
				}
			}

			tableData := pterm.TableData{
				{"ID", "Domain", "Environment", "Status", "Message"},
			}
			for _, d := range domains {
				icon := domainIcon(d.Status)
				msg := ""
				if d.Message != nil {
					msg = *d.Message
				}
				env := "—"
				if d.EnvironmentID != nil && *d.EnvironmentID != "" {
					if name, ok := envName[*d.EnvironmentID]; ok {
						env = name
					} else {
						env = *d.EnvironmentID
						if len(env) > 8 {
							env = env[:8]
						}
					}
				}
				tableData = append(tableData, []string{d.ID, d.Name, env, icon + " " + d.Status, msg})
			}

			if err := pterm.DefaultTable.WithHasHeader().WithData(tableData).Render(); err != nil {
				return err
			}
			fmt.Println()
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
