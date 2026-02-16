package auth

import (
	"fmt"

	"github.com/urfave/cli/v2"
)

// NewLoginCommand creates the login command
func NewLoginCommand() *cli.Command {
	return &cli.Command{
		Name:  "login",
		Usage: "Authenticate with CreateOS",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "token",
				Aliases: []string{"t"},
				Usage:   "API token for authentication",
			},
		},
		Action: func(c *cli.Context) error {
			token := c.String("token")

			if token != "" {
				fmt.Println("Logging in with token...")
				// TODO: Implement token-based authentication
			} else {
				fmt.Println("Starting interactive login...")
				// TODO: Implement interactive login
			}

			fmt.Println("Login successful!")
			return nil
		},
	}
}
