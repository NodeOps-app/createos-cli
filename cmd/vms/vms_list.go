package vms

import (
	"fmt"

	"github.com/pterm/pterm"
	"github.com/urfave/cli/v2"

	"github.com/NodeOps-app/createos-cli/internal/api"
)

func newVMListCommand() *cli.Command {
	return &cli.Command{
		Name:  "list",
		Usage: "List all VM terminal instances",
		Action: func(c *cli.Context) error {
			client, ok := c.App.Metadata[api.ClientKey].(*api.APIClient)
			if !ok {
				return fmt.Errorf("you're not signed in — run 'createos login' to get started")
			}

			vms, err := client.ListVMDeployments()
			if err != nil {
				return err
			}

			if len(vms) == 0 {
				fmt.Println("You don't have any VM instances yet.")
				return nil
			}

			tableData := pterm.TableData{
				{"ID", "Name", "Status", "IP Address", "Size", "Created At"},
			}
			for _, vm := range vms {
				name := "-"
				if vm.Name != nil {
					name = *vm.Name
				}
				ip := "-"
				if vm.Extra.IPAddress != "" {
					ip = vm.Extra.IPAddress
				}
				tableData = append(tableData, []string{
					vm.ID,
					name,
					vm.Status,
					ip,
					"-",
					vm.CreatedAt.Format("2006-01-02 15:04:05"),
				})
			}

			if err := pterm.DefaultTable.WithHasHeader().WithData(tableData).Render(); err != nil {
				return err
			}
			fmt.Println()
			return nil
		},
	}
}
