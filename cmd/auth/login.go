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
		Usage: "Sign in to your CreateOS account",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "token",
				Aliases: []string{"t"},
				Usage:   "Your API token (from your CreateOS dashboard)",
			},
		},
		Action: func(c *cli.Context) error {
			token := c.String("token")

			if token == "" {
				var err error
				token, err = pterm.DefaultInteractiveTextInput.
					WithMask("*").
					Show("Paste your API token (from your CreateOS dashboard)")
				if err != nil {
					return fmt.Errorf("could not read token: %w", err)
				}
			}

			if token == "" {
				return fmt.Errorf("token cannot be empty — please paste your API token from your CreateOS dashboard")
			}

			if err := config.SaveToken(token); err != nil {
				return fmt.Errorf("could not save your token: %w", err)
			}

			pterm.Success.Println("You're now signed in! Run 'createos whoami' to confirm your account.")
			return nil
		},
	}
}
