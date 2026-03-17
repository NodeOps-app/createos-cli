// Package version provides the version command.
package version

import (
	"github.com/urfave/cli/v2"

	"github.com/NodeOps-app/createos-cli/internal/intro"
)

// NewVersionCommand creates the version command.
func NewVersionCommand() *cli.Command {
	return &cli.Command{
		Name:  "version",
		Usage: "Print the current version",
		Action: func(_ *cli.Context) error {
			intro.Show()
			return nil
		},
	}
}
