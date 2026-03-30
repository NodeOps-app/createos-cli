package projects

import (
	"fmt"

	"github.com/pterm/pterm"
	"github.com/urfave/cli/v2"

	"github.com/NodeOps-app/createos-cli/internal/api"
)

func newGetCommand() *cli.Command {
	return &cli.Command{
		Name:      "get",
		Usage:     "Get a project by ID",
		ArgsUsage: "<project-id>",
		Action: func(c *cli.Context) error {
			if c.NArg() == 0 {
				return fmt.Errorf("please provide a project ID\n\n  To see your projects and their IDs, run:\n    createos projects list")
			}

			client, ok := c.App.Metadata[api.ClientKey].(*api.APIClient)
			if !ok {
				return fmt.Errorf("you're not signed in — run 'createos login' to get started")
			}

			id := c.Args().First()
			project, err := client.GetProject(id)
			if err != nil {
				return err
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

			fmt.Println()
			pterm.Println(pterm.Gray("  Tip: To manage deployments for this project, run:"))
			pterm.Println(pterm.Gray("    createos deployments list " + project.ID))

			return nil
		},
	}
}
