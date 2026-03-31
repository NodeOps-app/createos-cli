package users

import "github.com/urfave/cli/v2"

func newOAuthConsentsCommand() *cli.Command {
	return &cli.Command{
		Name:  "oauth-consents",
		Usage: "Manage OAuth app consents",
		Subcommands: []*cli.Command{
			newOAuthConsentsListCommand(),
			newOAuthConsentsRevokeCommand(),
		},
	}
}
