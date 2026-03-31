package users

import (
	"fmt"

	"github.com/pterm/pterm"
	"github.com/urfave/cli/v2"

	"github.com/NodeOps-app/createos-cli/internal/output"
)

func newOAuthConsentsListCommand() *cli.Command {
	return &cli.Command{
		Name:  "list",
		Usage: "List OAuth app consents",
		Action: func(c *cli.Context) error {
			client, err := getClient(c)
			if err != nil {
				return err
			}

			consents, err := client.ListOAuthConsents()
			if err != nil {
				return err
			}

			output.Render(c, consents, func() {
				if len(consents) == 0 {
					fmt.Println("You haven't granted access to any OAuth clients yet.")
					return
				}

				tableData := pterm.TableData{
					{"Client ID", "Client Name", "Client URL"},
				}
				for _, consent := range consents {
					clientID := "-"
					clientName := "-"
					clientURI := "-"
					if consent.ClientID != nil && *consent.ClientID != "" {
						clientID = *consent.ClientID
					}
					if consent.ClientName != nil && *consent.ClientName != "" {
						clientName = *consent.ClientName
					}
					if consent.ClientURI != nil && *consent.ClientURI != "" {
						clientURI = *consent.ClientURI
					}
					tableData = append(tableData, []string{clientID, clientName, clientURI})
				}
				_ = pterm.DefaultTable.WithHasHeader().WithData(tableData).Render()
				fmt.Println()
			})
			return nil
		},
	}
}
