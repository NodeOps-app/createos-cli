package oauth

import (
	"github.com/urfave/cli/v2"
)

func newInstructionsCommand() *cli.Command {
	return &cli.Command{
		Name:      "instructions",
		Usage:     "Show setup instructions for an OAuth client",
		ArgsUsage: "[client-id]",
		Description: "Shows the client details you need after registration, including redirect URIs,\n" +
			"whether the client is public, and the user info endpoint.",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "client", Usage: "OAuth client ID"},
		},
		Action: func(c *cli.Context) error {
			apiClient, err := getClient(c)
			if err != nil {
				return err
			}

			clientID, err := resolveOAuthClientID(c, apiClient)
			if err != nil {
				return err
			}

			detail, err := apiClient.GetOAuthClient(clientID)
			if err != nil {
				return err
			}

			printInstructions(c.String("api-url"), detail)
			return nil
		},
	}
}
