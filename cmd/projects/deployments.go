package projects

import (
	"github.com/urfave/cli/v2"
)

func newDeploymentsCommand() *cli.Command {
	return &cli.Command{
		Name:  "deployments",
		Usage: "Manage deployments for a project",
		Subcommands: []*cli.Command{
			newDeploymentsListCommand(),
			newDeploymentBuildLogsCommand(),
			newDeploymentLogsCommand(),
			newDeploymentRetriggerCommand(),
			newDeploymentDeleteCommand(),
			newDeploymentWakeupCommand(),
		},
	}
}
