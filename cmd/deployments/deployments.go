// Package deployments provides deployment management commands.
package deployments

import (
	"github.com/urfave/cli/v2"
)

// NewDeploymentsCommand returns the deployments command group.
func NewDeploymentsCommand() *cli.Command {
	return &cli.Command{
		Name:  "deployments",
		Usage: "Manage deployments for a project",
		Subcommands: []*cli.Command{
			newDeploymentsListCommand(),
			newDeploymentBuildLogsCommand(),
			newDeploymentLogsCommand(),
			newDeploymentPromoteCommand(),
			newDeploymentRetriggerCommand(),
			newDeploymentSleepCommand(),
			newDeploymentDeleteCommand(),
			newDeploymentWakeupCommand(),
		},
	}
}
