package auth

import (
	"fmt"
	"time"

	"github.com/pterm/pterm"
	"github.com/urfave/cli/v2"

	"github.com/NodeOps-app/createos-cli/internal/api"
)

// NewWhoamiCommand creates the whoami command
func NewWhoamiCommand() *cli.Command {
	return &cli.Command{
		Name:  "whoami",
		Usage: "Show the currently authenticated user",
		Action: func(c *cli.Context) error {
			client, ok := c.App.Metadata[api.ClientKey].(*api.APIClient)
			if !ok {
				return fmt.Errorf("you're not signed in — run 'createos login' to get started")
			}

			u, err := client.GetUser()
			if err != nil {
				return err
			}

			greeting := "Hey, " + u.Email + "!"
			if u.DisplayName != nil && *u.DisplayName != "" {
				greeting = "Hey, " + *u.DisplayName + "!"
			}

			createdAt, err := time.Parse(time.RFC3339Nano, u.CreatedAt)
			memberSince := u.CreatedAt
			if err == nil {
				memberSince = createdAt.Format("January 2, 2006")
			}

			fmt.Println()
			pterm.NewStyle(pterm.FgLightCyan, pterm.Bold).Printfln("  %s", greeting)
			fmt.Println()
			pterm.Printfln("  %s  %s", pterm.Gray("Email        "), u.Email)
			pterm.Printfln("  %s  %s", pterm.Gray("ID           "), u.ID)
			pterm.Printfln("  %s  %s", pterm.Gray("Member since "), memberSince)
			fmt.Println()

			return nil
		},
	}
}
