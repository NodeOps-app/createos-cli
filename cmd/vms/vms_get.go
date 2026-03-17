package vms

import (
	"fmt"

	"github.com/pterm/pterm"
	"github.com/urfave/cli/v2"

	"github.com/NodeOps-app/createos-cli/internal/api"
)

func newVMGetCommand() *cli.Command {
	return &cli.Command{
		Name:      "get",
		Usage:     "Get details for a VM instance",
		ArgsUsage: "<vm-id>",
		Action: func(c *cli.Context) error {
			if c.NArg() == 0 {
				return fmt.Errorf("please provide a VM ID\n\n  To see your VMs and their IDs, run:\n    createos vms list")
			}

			client, ok := c.App.Metadata[api.ClientKey].(*api.APIClient)
			if !ok {
				return fmt.Errorf("you're not signed in — run 'createos login' to get started")
			}

			id := c.Args().First()
			vm, err := client.GetVMDeployment(id)
			if err != nil {
				return err
			}

			cyan := pterm.NewStyle(pterm.FgCyan)

			cyan.Printf("ID:          ")
			fmt.Println(vm.ID)

			cyan.Printf("Name:        ")
			if vm.Name != nil {
				fmt.Println(*vm.Name)
			} else {
				fmt.Println("-")
			}

			cyan.Printf("Status:      ")
			fmt.Println(vm.Status)

			cyan.Printf("IP Address:  ")
			if vm.Extra.IPAddress != "" {
				fmt.Println(vm.Extra.IPAddress)
			} else {
				fmt.Println("-")
			}

			cyan.Printf("Created At:  ")
			fmt.Println(vm.CreatedAt.Format("2006-01-02 15:04:05"))

			cyan.Printf("Updated At:  ")
			fmt.Println(vm.UpdatedAt.Format("2006-01-02 15:04:05"))

			fmt.Println()
			if vm.Status == "deployed" && vm.Extra.IPAddress != "" {
				pterm.Println(pterm.Gray("  To connect via SSH: createos vms ssh " + vm.ID))
			} else if vm.Status == "deploying" {
				pterm.Println(pterm.Gray("  Your VM is still deploying. Check status with: createos vms get " + vm.ID))
			}

			return nil
		},
	}
}
