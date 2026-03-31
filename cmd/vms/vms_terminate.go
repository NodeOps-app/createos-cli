package vms

import (
	"fmt"

	"github.com/pterm/pterm"
	"github.com/urfave/cli/v2"

	"github.com/NodeOps-app/createos-cli/internal/api"
	"github.com/NodeOps-app/createos-cli/internal/terminal"
)

func newVMTerminateCommand() *cli.Command {
	return &cli.Command{
		Name:  "terminate",
		Usage: "Permanently destroy a VM terminal instance",
		Description: "Permanently destroys a VM and all its data. This action cannot be undone.\n\n" +
			"   To find your VM ID, run:\n" +
			"     createos vms list",
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
				return fmt.Errorf("non-interactive mode: use --force flag to confirm termination\n\n  Example:\n    createos vms terminate %s --force", id)
			}

			if terminal.IsInteractive() && !c.Bool("force") {
				pterm.Warning.Println("This will permanently destroy the VM and all its data.")
				confirm, err := pterm.DefaultInteractiveConfirm.
					WithDefaultText(fmt.Sprintf("Are you sure you want to permanently destroy VM %q? This cannot be undone", id)).
					WithDefaultValue(false).
					Show()
				if err != nil {
					return fmt.Errorf("could not read confirmation: %w", err)
				}
				if !confirm {
					fmt.Println("Cancelled. Your VM was not terminated.")
					return nil
				}
			}

			if err := client.TerminateVMDeployment(id); err != nil {
				return err
			}

			pterm.Success.Printf("VM %q has been terminated.\n", id)
			return nil
		},
	}
}
