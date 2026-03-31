package templates

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/pterm/pterm"
	"github.com/urfave/cli/v2"

	"github.com/NodeOps-app/createos-cli/internal/api"
	"github.com/NodeOps-app/createos-cli/internal/output"
)

func newTemplatesInfoCommand() *cli.Command {
	return &cli.Command{
		Name:      "info",
		Usage:     "Show details about a template",
		ArgsUsage: "[template-id]",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "template", Usage: "Template ID"},
		},
		Action: func(c *cli.Context) error {
			client, ok := c.App.Metadata[api.ClientKey].(*api.APIClient)
			if !ok {
				return fmt.Errorf("you're not signed in — run 'createos login' to get started")
			}

			id, err := resolveTemplate(c, client)
			if err != nil {
				return err
			}

			tmpl, err := client.GetTemplate(id)
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

			cyan.Printf("  Categories:  ")
			if len(tmpl.Categories) > 0 {
				fmt.Println(strings.Join(tmpl.Categories, ", "))
			} else {
				fmt.Println("-")
			}

			cyan.Printf("  Description: ")
			if tmpl.Description != "" {
				fmt.Println(tmpl.Description)
			} else {
				fmt.Println("-")
			}

			cyan.Printf("  Status:      ")
			fmt.Println(tmpl.Status)

			cyan.Printf("  Created:     ")
			fmt.Println(tmpl.CreatedAt.Format("2006-01-02 15:04:05"))

			fmt.Println()
			return nil
		},
	}
}
