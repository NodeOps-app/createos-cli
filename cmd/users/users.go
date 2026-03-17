package users

import "github.com/urfave/cli/v2"

// NewUsersCommand creates the users command with subcommands.
func NewUsersCommand() *cli.Command {
	return &cli.Command{
		Name:  "users",
		Usage: "Manage your user account",
		Subcommands: []*cli.Command{
			newOAuthConsentsCommand(),
		},
	}
}
