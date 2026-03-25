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

func newTemplatesInfoCommand() *cli.Command {
	return &cli.Command{
		Name:      "info",
		Usage:     "Show details about a template",
		ArgsUsage: "<template-id>",
		Action: func(c *cli.Context) error {
			if c.NArg() == 0 {
				return fmt.Errorf("please provide a template ID\n\n  To see available templates, run:\n    createos templates list")
			}

			client, ok := c.App.Metadata[api.ClientKey].(*api.APIClient)
			if !ok {
				return fmt.Errorf("you're not signed in — run 'createos login' to get started")
			}

			tmpl, err := client.GetTemplate(c.Args().First())
			if err != nil {
				return err
			}

			if output.IsJSON(c) {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(tmpl)
			}

			cyan := pterm.NewStyle(pterm.FgCyan)
			fmt.Println()
			cyan.Printf("  Name:        ")
			fmt.Println(tmpl.Name)

			cyan.Printf("  Type:        ")
			fmt.Println(tmpl.Type)

			cyan.Printf("  Description: ")
			if tmpl.Description != nil {
				fmt.Println(*tmpl.Description)
			} else {
				fmt.Println("-")
			}

			cyan.Printf("  Status:      ")
			fmt.Println(tmpl.Status)

			cyan.Printf("  Created:     ")
			fmt.Println(tmpl.CreatedAt.Format("2006-01-02 15:04:05"))

			fmt.Println()
			pterm.Println(pterm.Gray("  Use this template:"))
			pterm.Println(pterm.Gray("    createos templates use " + tmpl.ID))
			return nil
		},
	}
}
