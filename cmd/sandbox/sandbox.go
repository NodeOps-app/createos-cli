// Package sandbox holds the `createos sandbox` command tree —
// lifecycle, exec, files, networking, and disk management against the
// fc-spawn backend.
package sandbox

import (
	"github.com/urfave/cli/v2"
)

// NewSandboxCommand returns the parent `sandbox` group.
func NewSandboxCommand() *cli.Command {
	return &cli.Command{
		Name:    "sandbox",
		Aliases: []string{"sb"},
		Usage:   "Manage sandboxes",
		Subcommands: []*cli.Command{
			newCreateCommand(),
			newListCommand(),
			newGetCommand(),
			newRmCommand(),
			newEditCommand(),
			newPauseCommand(),
			newResumeCommand(),
			newForkCommand(),
			newExecCommand(),
			newPushCommand(),
			newPullCommand(),
			newShellCommand(),
			newSyncCommand(),
			newDCCommand(),
			newTunnelCommand(),
			newDiskCommand(),
			newNetworkCommand(),
			newFirewallCommand(),
			newTemplateCommand(),
			newShapesCommand(),
			newRootfsCommand(),
		},
	}
}
