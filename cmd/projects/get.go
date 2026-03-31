package projects

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/pterm/pterm"
	"github.com/urfave/cli/v2"

	"github.com/NodeOps-app/createos-cli/internal/api"
	"github.com/NodeOps-app/createos-cli/internal/cmdutil"
	"github.com/NodeOps-app/createos-cli/internal/output"
)

func newGetCommand() *cli.Command {
	return &cli.Command{
		Name:  "get",
		Usage: "Get a project by ID",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "project", Usage: "Project ID"},
		},
		Action: func(c *cli.Context) error {
			client, ok := c.App.Metadata[api.ClientKey].(*api.APIClient)
			if !ok {
				return fmt.Errorf("you're not signed in — run 'createos login' to get started")
			}

			id, err := cmdutil.ResolveProjectID(c.String("project"))
			if err != nil {
				return err
			}
			project, err := client.GetProject(id)
			if err != nil {
				return err
			}

			if output.IsJSON(c) {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(project)
			}

			cyan := pterm.NewStyle(pterm.FgCyan)
			cyan.Printf("ID:          ")
			fmt.Println(project.ID)

			cyan.Printf("Name:        ")
			fmt.Println(project.DisplayName)

			cyan.Printf("Type:        ")
			fmt.Println(project.Type)

			cyan.Printf("Description: ")
			if project.Description != nil {
				fmt.Println(*project.Description)
			} else {
				fmt.Println("")
			}

			cyan.Printf("Status:      ")
			fmt.Println(project.Status)

			cyan.Printf("Created At:  ")
			fmt.Println(project.CreatedAt.Format("2006-01-02 15:04:05"))

			cyan.Printf("Updated At:  ")
			fmt.Println(project.UpdatedAt.Format("2006-01-02 15:04:05"))

			return nil
		},
	}
}
