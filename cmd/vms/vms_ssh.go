package vms

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/pterm/pterm"
	"github.com/urfave/cli/v2"

	"github.com/NodeOps-app/createos-cli/internal/api"
	"github.com/NodeOps-app/createos-cli/internal/terminal"
)

// defaultFirewallRules returns the baseline firewall rules required for SSH access.
func defaultFirewallRules() []api.VMFirewallRule {
	return []api.VMFirewallRule{
		{Port: 22, Proto: "tcp", From: "0.0.0.0/0"},
	}
}

// findPublicKeys returns all *.pub files found in ~/.ssh/.
func findPublicKeys() map[string]string {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	matches, err := filepath.Glob(filepath.Join(home, ".ssh", "*.pub"))
	if err != nil || len(matches) == 0 {
		return nil
	}
	keys := make(map[string]string, len(matches))
	for _, path := range matches {
		data, err := os.ReadFile(path) //nolint:gosec
		if err != nil {
			continue
		}
		keys[filepath.Base(path)] = strings.TrimSpace(string(data))
	}
	return keys
}

// selectPublicKey prompts the user to pick a local SSH public key, or skip.
// Returns the selected key content, or "" if skipped.
func selectPublicKey() (string, error) {
	keys := findPublicKeys()
	if len(keys) == 0 {
		return "", nil
	}

	const skipOption = "None (skip)"
	options := make([]string, 0, len(keys)+1)
	nameByOption := make(map[string]string, len(keys))

	for name, content := range keys {
		// Show filename + key comment (last field) for readability.
		parts := strings.Fields(content)
		label := name
		if len(parts) >= 3 {
			label = fmt.Sprintf("%-30s  %s", name, parts[2])
		}
		options = append(options, label)
		nameByOption[label] = content
	}
	options = append(options, skipOption)

	selected, err := pterm.DefaultInteractiveSelect.
		WithOptions(options).
		WithDefaultText("Select an SSH public key to use for this session").
		Show()
	if err != nil {
		return "", fmt.Errorf("could not read selection: %w", err)
	}
	if selected == skipOption {
		return "", nil
	}
	return nameByOption[selected], nil
}

func newVMSSHCommand() *cli.Command {
	return &cli.Command{
		Name:      "ssh",
		Usage:     "Connect to a VM instance via SSH",
		ArgsUsage: "<vm-id>",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:  "user",
				Usage: "SSH user to connect as",
				Value: "root",
			},
		},
		Action: func(c *cli.Context) error {
			if !terminal.IsInteractive() {
				return fmt.Errorf("SSH requires an interactive terminal")
			}

			if c.NArg() == 0 {
				return fmt.Errorf("please provide a VM ID\n\n  To see your VMs and their IDs, run:\n    createos vms list")
			}

			client, ok := c.App.Metadata[api.ClientKey].(*api.APIClient)
			if !ok {
				return fmt.Errorf("you're not signed in — run 'createos login' to get started")
			}

			id := c.Args().First()
			user := c.String("user")

			vm, err := client.GetVMDeployment(id)
			if err != nil {
				return err
			}

			if vm.Status != "deployed" {
				return fmt.Errorf("VM %q is currently %q — it must be in 'deployed' state to connect\n\n  Check status with: createos vms get %s", id, vm.Status, id)
			}

			if vm.Extra.IPAddress == "" {
				return fmt.Errorf("VM does not have an IP address yet — try again in a moment\n\n  Check status with: createos vms get %s", id)
			}

			// Ask user to pick a local SSH public key for this session.
			localKey, err := selectPublicKey()
			if err != nil {
				return err
			}
			originalKeys := vm.Inputs.SSHKeys
			if originalKeys == nil {
				originalKeys = []string{}
			}
			firewallRules := vm.Inputs.FirewallRules
			if firewallRules == nil {
				firewallRules = defaultFirewallRules()
			}

			if localKey != "" {
				// Check if key is already present.
				alreadyPresent := false
				for _, k := range originalKeys {
					if strings.TrimSpace(k) == localKey {
						alreadyPresent = true
						break
					}
				}

				if !alreadyPresent {
					updatedKeys := append(originalKeys, localKey) //nolint:gocritic
					if err := client.UpdateVMDeployment(id, updatedKeys, firewallRules); err != nil {
						pterm.Warning.Println("Could not add your SSH key to the VM — you may need to add it manually.")
					} else {
						pterm.Info.Println("Your SSH public key has been added to the VM.")
						// Restore original keys on exit.
						defer func() {
							if err := client.UpdateVMDeployment(id, originalKeys, firewallRules); err != nil {
								pterm.Warning.Printf("Could not remove your SSH key from VM %q: %v\n", id, err)
							} else {
								pterm.Info.Println("Your SSH public key has been removed from the VM.")
							}
						}()
					}
				}
			}

			target := user + "@" + vm.Extra.IPAddress
			cmd := exec.CommandContext(context.Background(), "ssh", "-o", "StrictHostKeyChecking=accept-new", target) //nolint:gosec // target is user@<api-provided-ip>, intentional subprocess
			cmd.Stdin = os.Stdin
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr

			return cmd.Run()
		},
	}
}
