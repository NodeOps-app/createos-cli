package auth

import (
	"fmt"

	"github.com/NodeOps-app/createos-cli/internal/config"
	"github.com/pterm/pterm"
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

			if token == "" {
				var err error
				token, err = pterm.DefaultInteractiveTextInput.
					WithMask("*").
					Show("Enter your API token")
				if err != nil {
					return fmt.Errorf("failed to read token: %w", err)
				}
			}

			if token == "" {
				return fmt.Errorf("token cannot be empty")
			}

			if err := config.SaveToken(token); err != nil {
				return fmt.Errorf("failed to save token: %w", err)
			}

			pterm.Success.Println("Login successful! Token saved to ~/.createos/.token")
			return nil
		},
	}
}
