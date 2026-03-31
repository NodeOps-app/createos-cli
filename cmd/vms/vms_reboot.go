package vms

import (
	"fmt"

	"github.com/pterm/pterm"
	"github.com/urfave/cli/v2"

	"github.com/NodeOps-app/createos-cli/internal/api"
	"github.com/NodeOps-app/createos-cli/internal/terminal"
)

func newVMRebootCommand() *cli.Command {
	return &cli.Command{
		Name:  "reboot",
		Usage: "Reboot a VM terminal instance",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "vm", Usage: "VM ID"},
			&cli.BoolFlag{
				Name:  "force",
				Usage: "Skip confirmation prompt (required in non-interactive mode)",
			},
		},
		Action: func(c *cli.Context) error {
			client, ok := c.App.Metadata[api.ClientKey].(*api.APIClient)
			if !ok {
				return fmt.Errorf("you're not signed in — run 'createos login' to get started")
			}

			id, err := resolveVM(c, client)
			if err != nil {
				return err
			}

			if !terminal.IsInteractive() && !c.Bool("force") {
				return fmt.Errorf("use --force to confirm reboot\n\n  Example:\n    createos vms reboot %s --force", id)
			}

			if terminal.IsInteractive() && !c.Bool("force") {
				confirm, err := pterm.DefaultInteractiveConfirm.
					WithDefaultText(fmt.Sprintf("Reboot VM %q? The VM will be briefly unavailable.", id)).
					WithDefaultValue(false).
					Show()
				if err != nil {
					return fmt.Errorf("could not read confirmation: %w", err)
				}
				if !confirm {
					fmt.Println("Cancelled. Your VM was not rebooted.")
					return nil
				}
			}

			if err := client.RebootVMDeployment(id); err != nil {
				return err
			}

			pterm.Success.Printf("Reboot initiated for VM %q.\n", id)
			return nil
		},
	}
}
