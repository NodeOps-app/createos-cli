// Package templates provides template browsing and scaffolding commands.
package templates

import (
	"github.com/urfave/cli/v2"
)

// NewTemplatesCommand returns the templates command group.
func NewTemplatesCommand() *cli.Command {
	return &cli.Command{
		Name:  "templates",
		Usage: "Browse and scaffold from project templates",
		Subcommands: []*cli.Command{
			newTemplatesListCommand(),
			newTemplatesInfoCommand(),
			newTemplatesUseCommand(),
		},
	}
}
