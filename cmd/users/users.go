package users

import "github.com/urfave/cli/v2"

// NewUsersCommand creates the me command with subcommands.
func NewUsersCommand() *cli.Command {
	return &cli.Command{
		Name:  "me",
		Usage: "Manage your account and OAuth consents",
		Subcommands: []*cli.Command{
			newOAuthConsentsCommand(),
		},
	}
}
