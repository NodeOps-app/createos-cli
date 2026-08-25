package oauth

import (
	"fmt"

	"github.com/pterm/pterm"
	"github.com/urfave/cli/v2"

	"github.com/NodeOps-app/createos-cli/internal/output"
)

func newListCommand() *cli.Command {
	return &cli.Command{
		Name:  "list",
		Usage: "List your OAuth clients",
		Action: func(c *cli.Context) error {
			client, err := getClient(c)
			if err != nil {
				return err
			}

			clients, err := client.ListOAuthClients()
			if err != nil {
				return err
			}

			output.Render(c, clients, func() {
				if len(clients) == 0 {
					fmt.Println("You don't have any OAuth clients yet.")
					return
				}

				tableData := pterm.TableData{
					{"ID", "Name", "Created At"},
				}
				for _, item := range clients {
					tableData = append(tableData, []string{
						item.ID,
						item.Name,
						item.CreatedAt.Local().Format("2006-01-02 15:04:05"),
					})
				}
				if err := output.RenderTable(tableData); err != nil {
					pterm.Error.Println("could not display table")
				}
				fmt.Println()
			})
			return nil
		},
	}
}
