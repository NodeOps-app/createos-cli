// Package vms provides VM terminal management commands.
package vms

import "github.com/urfave/cli/v2"

// NewVMsCommand creates the vms command group.
func NewVMsCommand() *cli.Command {
	return &cli.Command{
		Name:  "vms",
		Usage: "Manage VM terminal instances",
		Subcommands: []*cli.Command{
			newVMDeployCommand(),
			newVMGetCommand(),
			newVMListCommand(),
			newVMRebootCommand(),
			newVMResizeCommand(),
			newVMSSHCommand(),
			newVMTerminateCommand(),
		},
	}
}
