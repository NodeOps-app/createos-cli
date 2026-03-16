package projects

import (
	"github.com/urfave/cli/v2"
)

func newDomainsCommand() *cli.Command {
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
