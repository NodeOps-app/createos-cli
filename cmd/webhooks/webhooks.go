package webhooks

import "github.com/urfave/cli/v2"

// NewWebhooksCommand returns the top-level "webhooks" command with its subcommands.
func NewWebhooksCommand() *cli.Command {
	return &cli.Command{
		Name:  "webhooks",
		Usage: "Manage webhook endpoints",
		Subcommands: []*cli.Command{
			newWebhooksListCommand(),
			newWebhooksGetCommand(),
			newWebhooksCreateCommand(),
			newWebhooksDeleteCommand(),
			newWebhooksSuspendCommand(),
			newWebhooksResumeCommand(),
		},
	}
}
