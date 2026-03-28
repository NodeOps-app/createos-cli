package oauth

import "github.com/urfave/cli/v2"

// NewOAuthCommand creates the oauth-clients command with subcommands.
func NewOAuthCommand() *cli.Command {
	return &cli.Command{
		Name:  "oauth-clients",
		Usage: "Manage OAuth clients",
		Subcommands: []*cli.Command{
			newListCommand(),
			newCreateCommand(),
			newInstructionsCommand(),
		},
	}
}
