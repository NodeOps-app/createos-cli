package vms

import (
	"fmt"
	"strings"
	"time"

	"github.com/pterm/pterm"
	"github.com/urfave/cli/v2"

	"github.com/NodeOps-app/createos-cli/internal/api"
	"github.com/NodeOps-app/createos-cli/internal/terminal"
)

func newVMDeployCommand() *cli.Command {
	return &cli.Command{
		Name:  "deploy",
		Usage: "Deploy a new VM terminal instance",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:  "name",
				Usage: "Optional name for the VM",
			},
			&cli.StringFlag{
				Name:  "zone",
				Usage: "Deployment zone (e.g. nyc3, sfo3, sgp1)",
			},
			&cli.IntFlag{
				Name:  "size",
				Usage: "VM size index from the available sizes list (1-based)",
			},
			&cli.StringSliceFlag{
				Name:  "ssh-key",
				Usage: "SSH public key to add (repeatable, optional)",
			},
		},
		Action: func(c *cli.Context) error {
			client, ok := c.App.Metadata[api.ClientKey].(*api.APIClient)
			if !ok {
				return fmt.Errorf("you're not signed in — run 'createos login' to get started")
			}

			// Fetch available sizes from API
			sizes, err := client.GetVMSizes()
			if err != nil {
				return fmt.Errorf("could not fetch available VM sizes: %w", err)
			}
			if len(sizes) == 0 {
				return fmt.Errorf("no VM sizes are currently available")
			}

			var name, zone string
			var sshKeys []string
			var size api.VMSize

			isInteractive := terminal.IsInteractive()
			hasFlags := c.IsSet("size") || c.IsSet("zone")

			if isInteractive && !hasFlags {
				// Fetch zones from API
				zones, err := client.GetDOZones()
				if err != nil {
					return fmt.Errorf("could not fetch available zones: %w", err)
				}

				// Name
				nameInput, err := pterm.DefaultInteractiveTextInput.
					WithDefaultText("VM name (optional, press Enter to skip)").
					Show()
				if err != nil {
					return fmt.Errorf("could not read name: %w", err)
				}
				name = strings.TrimSpace(nameInput)

				// Zone
				zoneOptions := make([]string, len(zones))
				for i, z := range zones {
					zoneOptions[i] = fmt.Sprintf("%-6s  %s, %s", z.Name, z.Region, z.Country)
				}
				zoneSelected, err := pterm.DefaultInteractiveSelect.
					WithOptions(zoneOptions).
					WithDefaultText("Select a zone").
					Show()
				if err != nil {
					return fmt.Errorf("could not read zone: %w", err)
				}
				zone = strings.SplitN(strings.TrimSpace(zoneSelected), " ", 2)[0]

				// Size
				sizeOptions := make([]string, len(sizes))
				for i, s := range sizes {
					sizeOptions[i] = formatVMSize(i+1, s)
				}
				sizeSelected, err := pterm.DefaultInteractiveSelect.
					WithOptions(sizeOptions).
					WithDefaultText("Select a VM size").
					Show()
				if err != nil {
					return fmt.Errorf("could not read size: %w", err)
				}
				size = sizes[indexFromOption(sizeSelected, sizeOptions)]

				// SSH keys (optional)
				addKey, err := pterm.DefaultInteractiveConfirm.
					WithDefaultText("Add an SSH public key?").
					WithDefaultValue(false).
					Show()
				if err != nil {
					return fmt.Errorf("could not read confirmation: %w", err)
				}
				for addKey {
					keyInput, err := pterm.DefaultInteractiveTextInput.
						WithDefaultText("Paste your SSH public key").
						Show()
					if err != nil {
						return fmt.Errorf("could not read SSH key: %w", err)
					}
					key := strings.TrimSpace(keyInput)
					if key != "" {
						sshKeys = append(sshKeys, key)
					}
					addKey, err = pterm.DefaultInteractiveConfirm.
						WithDefaultText("Add another SSH key?").
						WithDefaultValue(false).
						Show()
					if err != nil {
						return fmt.Errorf("could not read confirmation: %w", err)
					}
				}

				// Summary
				pterm.Info.Println("Deploying VM with the following configuration:")
				fmt.Printf("  Zone:     %s\n", zone)
				fmt.Printf("  Size:     %s\n", formatVMSize(0, size))
				fmt.Printf("  SSH Keys: %d key(s)\n", len(sshKeys))
				if name != "" {
					fmt.Printf("  Name:     %s\n", name)
				}
				fmt.Println()
			} else {
				// Non-TTY or flags provided
				name = c.String("name")
				zone = c.String("zone")
				if zone == "" {
					return fmt.Errorf("please provide a zone with --zone\n\n  Example:\n    createos vms deploy --size 1 --zone nyc3")
				}

				sizeIndex := c.Int("size")
				if sizeIndex == 0 {
					sizeList := vmSizeList(sizes)
					return fmt.Errorf("please provide a size index with --size\n\n%s\n\n  Example:\n    createos vms deploy --size 1 --zone nyc3", sizeList)
				}
				if sizeIndex < 1 || sizeIndex > len(sizes) {
					return fmt.Errorf("size index %d is out of range (1–%d)", sizeIndex, len(sizes))
				}
				size = sizes[sizeIndex-1]
				sshKeys = c.StringSlice("ssh-key")
			}

			pterm.Info.Println("VM creation can take 5–15 minutes. Please wait...")
			fmt.Println()

			spinner, err := pterm.DefaultSpinner.Start("Deploying your VM...")
			if err != nil {
				return fmt.Errorf("could not start spinner: %w", err)
			}

			vm, err := client.CreateVMDeployment(name, zone, sshKeys, size)
			if err != nil {
				spinner.Fail("Deployment request failed")
				return err
			}

			// Poll until deployed or terminated (max ~10 minutes)
			vmID := vm.ID
			const maxPolls = 200
			for i := 0; i < maxPolls; i++ {
				time.Sleep(3 * time.Second)

				updated, err := client.GetVMDeployment(vmID)
				if err != nil {
					spinner.Fail("Failed to check deployment status")
					return err
				}

				if updated.Status == "deployed" {
					spinner.Success("VM deployed successfully!")
					fmt.Println()
					pterm.Success.Printf("VM ID:      %s\n", updated.ID)
					if updated.Extra.IPAddress != "" {
						pterm.Success.Printf("IP Address: %s\n", updated.Extra.IPAddress)
					}
					fmt.Println()
					pterm.Println(pterm.Gray("  Connect via SSH: createos vms ssh " + updated.ID))
					return nil
				}

				if updated.Status == "terminated" {
					spinner.Fail("VM deployment was terminated unexpectedly.")
					return fmt.Errorf("VM deployment failed — the VM was terminated during provisioning")
				}
			}

			spinner.Warning("Deployment is taking longer than expected.")
			fmt.Println()
			pterm.Println(pterm.Gray("  Check the status with: createos vms get " + vmID))
			return nil
		},
	}
}

// formatVMSize returns a human-readable label for a VM size.
// Pass index=0 to omit the index prefix.
func formatVMSize(index int, s api.VMSize) string {
	cpu := s.CPU / 1000
	if cpu == 0 {
		cpu = 1
	}
	if index > 0 {
		return fmt.Sprintf("[%d]  %d vCPU, %d MiB RAM, %d MiB disk", index, cpu, s.MemoryMiB, s.DiskMiB)
	}
	return fmt.Sprintf("%d vCPU, %d MiB RAM, %d MiB disk", cpu, s.MemoryMiB, s.DiskMiB)
}

// vmSizeList returns a formatted list of sizes for error messages.
func vmSizeList(sizes []api.VMSize) string {
	var sb strings.Builder
	sb.WriteString("  Available sizes:\n")
	for i, s := range sizes {
		sb.WriteString("    " + formatVMSize(i+1, s) + "\n")
	}
	return sb.String()
}

// indexFromOption returns the 0-based index of the selected option.
func indexFromOption(selected string, options []string) int {
	for i, o := range options {
		if o == selected {
			return i
		}
	}
	return 0
}
