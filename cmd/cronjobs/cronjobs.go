// Package cronjobs provides cron job management commands.
package cronjobs

import "github.com/urfave/cli/v2"

// NewCronjobsCommand returns the cronjobs command group.
func NewCronjobsCommand() *cli.Command {
	return &cli.Command{
		Name:  "cronjobs",
		Usage: "Manage cron jobs for a project",
		Subcommands: []*cli.Command{
			newCronjobsActivitiesCommand(),
			newCronjobsCreateCommand(),
			newCronjobsDeleteCommand(),
			newCronjobsGetCommand(),
			newCronjobsListCommand(),
			newCronjobsSuspendCommand(),
			newCronjobsUnsuspendCommand(),
			newCronjobsUpdateCommand(),
		},
	}
}
