// Package version provides the version command.
package version

import (
	"fmt"

	"github.com/urfave/cli/v2"

	"github.com/NodeOps-app/createos-cli/internal/intro"
	"github.com/NodeOps-app/createos-cli/internal/pkg/version"
)

// NewVersionCommand creates the version command.
func NewVersionCommand() *cli.Command {
	return &cli.Command{
		Name:  "version",
		Usage: "Print the current version",
		Action: func(_ *cli.Context) error {
			intro.Show()
			fmt.Printf("  Version: %s\n  Channel: %s\n  Commit:  %s\n\n", version.Version, version.Channel, version.Commit)
			return nil
		},
	}
}
