package oauth

import (
	"fmt"

	"github.com/urfave/cli/v2"
)

func newInstructionsCommand() *cli.Command {
	return &cli.Command{
		Name:      "instructions",
		Usage:     "Show setup instructions for an OAuth client",
		ArgsUsage: "<client-id>",
		Description: "Shows the client details you need after registration, including redirect URIs,\n" +
			"whether the client is public, and the user info endpoint.",
		Action: func(c *cli.Context) error {
			if c.NArg() == 0 {
				return fmt.Errorf("please provide a client ID\n\n  To see your client IDs, run:\n    createos oauth clients list")
			}

			client, err := getClient(c)
			if err != nil {
				return err
			}

			detail, err := client.GetOAuthClient(c.Args().First())
			if err != nil {
				return err
			}

			printInstructions(c.String("api-url"), detail)
			return nil
		},
	}
}
