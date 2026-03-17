package oauth

import "github.com/urfave/cli/v2"

func newClientsCommand() *cli.Command {
	return &cli.Command{
		Name:  "clients",
		Usage: "Manage OAuth clients",
		Subcommands: []*cli.Command{
			newCreateCommand(),
			newInstructionsCommand(),
			newListCommand(),
		},
	}
}
