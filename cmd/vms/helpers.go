package vms

import (
	"fmt"

	"github.com/pterm/pterm"
	"github.com/urfave/cli/v2"

	"github.com/NodeOps-app/createos-cli/internal/api"
	"github.com/NodeOps-app/createos-cli/internal/terminal"
)

// resolveVM resolves a VM ID from flags or interactively.
func resolveVM(c *cli.Context, client *api.APIClient) (string, error) {
	if vmID := c.String("vm"); vmID != "" {
		return vmID, nil
	}
	if !terminal.IsInteractive() {
		return "", fmt.Errorf("please provide a VM ID\n\n  Example:\n    createos vms %s --vm <vm-id>", c.Command.Name)
	}
	return pickVM(client)
}

func pickVM(client *api.APIClient) (string, error) {
	vms, err := client.ListVMDeployments()
	if err != nil {
		return "", err
	}
	if len(vms) == 0 {
		return "", fmt.Errorf("no VM instances found")
	}
	if len(vms) == 1 {
		return vms[0].ID, nil
	}

	options := make([]string, len(vms))
	for i, vm := range vms {
		name := vm.ID
		if vm.Name != nil && *vm.Name != "" {
			name = *vm.Name
		}
		ip := "-"
		if vm.Extra.IPAddress != "" {
			ip = vm.Extra.IPAddress
		}
		options[i] = fmt.Sprintf("%s  %s  %s", name, ip, vm.Status)
	}

	selected, err := pterm.DefaultInteractiveSelect.
		WithOptions(options).
		WithDefaultText("Select a VM").
		Show()
	if err != nil {
		return "", fmt.Errorf("could not read selection: %w", err)
	}
	for i, opt := range options {
		if opt == selected {
			return vms[i].ID, nil
		}
	}
	return "", fmt.Errorf("no VM selected")
}
