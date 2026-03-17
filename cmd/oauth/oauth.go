package oauth

import "github.com/urfave/cli/v2"

func NewOAuthCommand() *cli.Command {
	return &cli.Command{
		Name:  "oauth",
		Usage: "Manage OAuth clients",
		Subcommands: []*cli.Command{
			newClientsCommand(),
		},
	}
}
