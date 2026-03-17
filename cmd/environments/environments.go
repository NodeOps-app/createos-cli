package environments

import "github.com/urfave/cli/v2"

func NewEnvironmentsCommand() *cli.Command {
	return &cli.Command{
		Name:  "environments",
		Usage: "Manage environments for a project",
		Subcommands: []*cli.Command{
			newEnvironmentsDeleteCommand(),
			newEnvironmentsListCommand(),
		},
	}
}
