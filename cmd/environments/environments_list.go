package environments

import (
	"fmt"
	"strings"

	"github.com/pterm/pterm"
	"github.com/urfave/cli/v2"

	"github.com/NodeOps-app/createos-cli/internal/api"
)

func newEnvironmentsListCommand() *cli.Command {
	return &cli.Command{
		Name:      "list",
		Usage:     "List environments for a project",
		ArgsUsage: "<project-id>",
		Action: func(c *cli.Context) error {
			if c.NArg() == 0 {
				return fmt.Errorf("please provide a project ID\n\n  To see your projects and their IDs, run:\n    createos projects list")
			}

			client, ok := c.App.Metadata[api.ClientKey].(*api.APIClient)
			if !ok {
				return fmt.Errorf("you're not signed in — run 'createos login' to get started")
			}

			projectID := c.Args().First()
			environments, err := client.ListEnvironments(projectID)
			if err != nil {
				return err
			}

			if len(environments) == 0 {
				fmt.Println("No environments found for this project.")
				return nil
			}

			tableData := pterm.TableData{
				{"ID", "Name", "Branch", "Status", "URL", "Domains", "Created At"},
			}
			for _, env := range environments {
				branch := "-"
				if env.Branch != nil && *env.Branch != "" {
					branch = *env.Branch
				}

				url := env.Extra.Endpoint
				domains := "-"
				if len(env.Extra.CustomDomains) > 0 {
					domains = strings.Join(env.Extra.CustomDomains, ", ")
				}

				tableData = append(tableData, []string{
					env.ID,
					env.DisplayName,
					branch,
					env.Status,
					url,
					domains,
					env.CreatedAt.Format("2006-01-02 15:04:05"),
				})
			}

			if err := pterm.DefaultTable.WithHasHeader().WithData(tableData).Render(); err != nil {
				return err
			}
			fmt.Println()
			pterm.Println(pterm.Gray("  Tip: To delete an environment, run:"))
			pterm.Println(pterm.Gray("    createos environments delete " + projectID + " <environment-id>"))
			return nil
		},
	}
}
