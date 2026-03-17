package projects

import "github.com/urfave/cli/v2"

func newEnvironmentsCommand() *cli.Command {
	return &cli.Command{
		Name:  "environments",
		Usage: "Manage environments for a project",
		Subcommands: []*cli.Command{
			newEnvironmentsDeleteCommand(),
			newEnvironmentsListCommand(),
		},
	}
}
