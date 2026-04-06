package projects

import (
	"github.com/urfave/cli/v2"
)

// NewProjectsCommand creates the projects command with subcommands.
func NewProjectsCommand() *cli.Command {
	return &cli.Command{
		Name:  "projects",
		Usage: "Manage projects",
		Subcommands: []*cli.Command{
			newAddCommand(),
			newDeleteCommand(),
			newGetCommand(),
			newListCommand(),
		},
	}
}
