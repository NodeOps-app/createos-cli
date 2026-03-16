package version

import (
	"github.com/NodeOps-app/createos-cli/internal/intro"
	"github.com/urfave/cli/v2"
)

func NewVersionCommand() *cli.Command {
	return &cli.Command{
		Name:  "version",
		Usage: "Print the current version",
		Action: func(c *cli.Context) error {
			intro.Show()
			return nil
		},
	}
}
