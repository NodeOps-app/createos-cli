package vms

import (
	"fmt"

	"github.com/pterm/pterm"
	"github.com/urfave/cli/v2"

	"github.com/NodeOps-app/createos-cli/internal/api"
	"github.com/NodeOps-app/createos-cli/internal/terminal"
)

func newVMResizeCommand() *cli.Command {
	return &cli.Command{
		Name:      "resize",
		Usage:     "Resize a VM terminal instance",
		ArgsUsage: "[vm-id]",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "vm", Usage: "VM ID"},
			&cli.IntFlag{
				Name:  "size",
				Usage: "New VM size index from the available sizes list (1-based)",
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

			vm, err := client.GetVMDeployment(id)
			if err != nil {
				return err
			}

			if vm.Status != "deployed" {
				return fmt.Errorf("VM %q is currently %q — it must be in 'deployed' state to resize", id, vm.Status)
			}

			sizes, err := client.GetVMSizes()
			if err != nil {
				return fmt.Errorf("could not fetch available VM sizes: %w", err)
			}
			if len(sizes) == 0 {
				return fmt.Errorf("no VM sizes are currently available")
			}

			var size api.VMSize

			if terminal.IsInteractive() && !c.IsSet("size") {
				sizeOptions := make([]string, len(sizes))
				for i, s := range sizes {
					sizeOptions[i] = formatVMSize(i+1, s)
				}
				sizeSelected, err := pterm.DefaultInteractiveSelect.
					WithOptions(sizeOptions).
					WithDefaultText("Select the new VM size").
					Show()
				if err != nil {
					return fmt.Errorf("could not read size: %w", err)
				}
				size = sizes[indexFromOption(sizeSelected, sizeOptions)]
			} else {
				sizeIndex := c.Int("size")
				if sizeIndex == 0 {
					sizeList := vmSizeList(sizes)
					return fmt.Errorf("please provide a size index with --size\n\n%s\n\n  Example:\n    createos vms resize %s --size 1", sizeList, id)
				}
				if sizeIndex < 1 || sizeIndex > len(sizes) {
					return fmt.Errorf("size index %d is out of range (1–%d)", sizeIndex, len(sizes))
				}
				size = sizes[sizeIndex-1]
			}

			if err := client.ResizeVMDeployment(id, size); err != nil {
				return err
			}

			pterm.Success.Printf("Resize request submitted for VM %q.\n", id)
			return nil
		},
	}
}
