package vms

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/pterm/pterm"
	"github.com/urfave/cli/v2"
	gossh "golang.org/x/crypto/ssh"

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

// generateTempSSHKeypair creates a temporary ed25519 keypair, writes the private key
// to a temp file, and returns the public key string, the private key path, and a
// cleanup func that removes the temp file.
func generateTempSSHKeypair() (publicKey string, privateKeyPath string, cleanup func(), err error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return "", "", nil, fmt.Errorf("could not generate keypair: %w", err)
	}

	// Marshal public key to authorized_keys format.
	sshPub, err := gossh.NewPublicKey(pub)
	if err != nil {
		return "", "", nil, fmt.Errorf("could not encode public key: %w", err)
	}
	pubKeyStr := strings.TrimSpace(string(gossh.MarshalAuthorizedKey(sshPub)))

	// Marshal private key to OpenSSH PEM format.
	privPEM, err := gossh.MarshalPrivateKey(priv, "")
	if err != nil {
		return "", "", nil, fmt.Errorf("could not encode private key: %w", err)
	}
	privBytes := pem.EncodeToMemory(privPEM)

	// Write private key to a temp file with restricted permissions.
	f, err := os.CreateTemp("", "createos-vm-*.pem")
	if err != nil {
		return "", "", nil, fmt.Errorf("could not create temp key file: %w", err)
	}
	if err := os.Chmod(f.Name(), 0600); err != nil {
		_ = f.Close()
		_ = os.Remove(f.Name())
		return "", "", nil, fmt.Errorf("could not set key file permissions: %w", err)
	}
	if _, err := f.Write(privBytes); err != nil {
		_ = f.Close()
		_ = os.Remove(f.Name())
		return "", "", nil, fmt.Errorf("could not write private key: %w", err)
	}
	_ = f.Close()

	cleanup = func() { _ = os.Remove(f.Name()) }
	return pubKeyStr, f.Name(), cleanup, nil
}

const (
	optionTempKey = "Generate a temporary key (auto-deleted on exit)"
	optionSkip    = "None (skip)"
)

// selectPublicKey prompts the user to pick a local SSH public key, generate a temp
// key, or skip. Returns the public key content and an optional private key path
// (non-empty only for generated temp keys).
func selectPublicKey() (publicKey string, privateKeyPath string, err error) {
	keys := findPublicKeys()

	options := make([]string, 0, len(keys)+2)
	nameByOption := make(map[string]string, len(keys))

	for name, content := range keys {
		parts := strings.Fields(content)
		label := name
		if len(parts) >= 3 {
			label = fmt.Sprintf("%-30s  %s", name, parts[2])
		}
		options = append(options, label)
		nameByOption[label] = content
	}
	options = append(options, optionTempKey, optionSkip)

	selected, err := pterm.DefaultInteractiveSelect.
		WithOptions(options).
		WithDefaultText("Select an SSH public key to use for this session").
		Show()
	if err != nil {
		return "", "", fmt.Errorf("could not read selection: %w", err)
	}

	switch selected {
	case optionSkip:
		return "", "", nil
	case optionTempKey:
		pub, privPath, _, genErr := generateTempSSHKeypair()
		if genErr != nil {
			return "", "", genErr
		}
		return pub, privPath, nil
	default:
		return nameByOption[selected], "", nil
	}
}

func newVMSSHCommand() *cli.Command {
	return &cli.Command{
		Name:  "ssh",
		Usage: "Connect to a VM instance via SSH",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "vm", Usage: "VM ID"},
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

			client, ok := c.App.Metadata[api.ClientKey].(*api.APIClient)
			if !ok {
				return fmt.Errorf("you're not signed in — run 'createos login' to get started")
			}

			id, err := resolveVM(c, client)
			if err != nil {
				return err
			}
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

			localKey, privateKeyPath, err := selectPublicKey()
			if err != nil {
				return err
			}

			// Clean up temp private key file on exit if one was generated.
			if privateKeyPath != "" {
				defer os.Remove(privateKeyPath) //nolint:errcheck
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
						pterm.Warning.Printf("Could not add your SSH key to the VM: %v\n", err)
					} else {
						pterm.Info.Println("Your SSH public key has been added to the VM.")
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
			sshArgs := []string{"-o", "StrictHostKeyChecking=accept-new"}
			if privateKeyPath != "" {
				sshArgs = append(sshArgs, "-i", privateKeyPath)
			}
			sshArgs = append(sshArgs, target)

			cmd := exec.CommandContext(context.Background(), "ssh", sshArgs...) //nolint:gosec
			cmd.Stdin = os.Stdin
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr

			return cmd.Run()
		},
	}
}
