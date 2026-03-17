package oauth

import (
	"fmt"

	"github.com/pterm/pterm"
	"github.com/urfave/cli/v2"
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

			if len(clients) == 0 {
				fmt.Println("You don't have any OAuth clients yet.")
				fmt.Println()
				pterm.Println(pterm.Gray("  Hint: To create one, run:"))
				pterm.Println(pterm.Gray("    createos oauth clients create"))
				return nil
			}

			tableData := pterm.TableData{
				{"ID", "Name", "Created At"},
			}
			for _, item := range clients {
				tableData = append(tableData, []string{
					item.ID,
					item.Name,
					item.CreatedAt.Format("2006-01-02 15:04:05"),
				})
			}

			if err := pterm.DefaultTable.WithHasHeader().WithData(tableData).Render(); err != nil {
				return err
			}
			fmt.Println()
			pterm.Println(pterm.Gray("  Hint: To see setup instructions for a client, run:"))
			pterm.Println(pterm.Gray("    createos oauth clients instructions <client-id>"))
			return nil
		},
	}
}
