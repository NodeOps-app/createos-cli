// Package domains provides custom domain management commands.
package domains

import (
	"github.com/urfave/cli/v2"
)

// NewDomainsCommand returns the domains command group.
func NewDomainsCommand() *cli.Command {
	return &cli.Command{
		Name:  "domains",
		Usage: "Manage custom domains for a project",
		Subcommands: []*cli.Command{
			newDomainsListCommand(),
			newDomainsAddCommand(),
			newDomainsRefreshCommand(),
			newDomainsDeleteCommand(),
		},
	}
}
