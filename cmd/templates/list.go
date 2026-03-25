package templates

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/pterm/pterm"
	"github.com/urfave/cli/v2"

	"github.com/NodeOps-app/createos-cli/internal/api"
	"github.com/NodeOps-app/createos-cli/internal/output"
)

func newTemplatesListCommand() *cli.Command {
	return &cli.Command{
		Name:  "list",
		Usage: "List available project templates",
		Action: func(c *cli.Context) error {
			client, ok := c.App.Metadata[api.ClientKey].(*api.APIClient)
			if !ok {
				return fmt.Errorf("you're not signed in — run 'createos login' to get started")
			}

			templates, err := client.ListPublishedTemplates()
			if err != nil {
				return err
			}

			if output.IsJSON(c) {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(templates)
			}

			if len(templates) == 0 {
				fmt.Println("No templates available yet.")
				return nil
			}

			tableData := pterm.TableData{
				{"ID", "Name", "Type", "Description"},
			}
			for _, t := range templates {
				desc := ""
				if t.Description != nil {
					desc = *t.Description
					if len(desc) > 60 {
						desc = desc[:57] + "..."
					}
				}
				tableData = append(tableData, []string{
					t.ID,
					t.Name,
					t.Type,
					desc,
				})
			}

			if err := pterm.DefaultTable.WithHasHeader().WithData(tableData).Render(); err != nil {
				return err
			}
			fmt.Println()
			pterm.Println(pterm.Gray("  Tip: To use a template, run:"))
			pterm.Println(pterm.Gray("    createos templates use <template-id>"))
			return nil
		},
	}
}
