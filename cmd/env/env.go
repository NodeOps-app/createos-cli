// Package env provides environment variable management commands.
package env

import (
	"github.com/urfave/cli/v2"
)

// NewEnvCommand returns the env command group.
func NewEnvCommand() *cli.Command {
	return &cli.Command{
		Name:  "env",
		Usage: "Manage environment variables for a project",
		Subcommands: []*cli.Command{
			newEnvListCommand(),
			newEnvSetCommand(),
			newEnvRmCommand(),
			newEnvPullCommand(),
			newEnvPushCommand(),
		},
	}
}
