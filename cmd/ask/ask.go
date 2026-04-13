// Package ask provides the AI assistant command powered by OpenCode.
package ask

import (
	"context"
	_ "embed"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/pterm/pterm"
	"github.com/urfave/cli/v2"

	"github.com/NodeOps-app/createos-cli/internal/terminal"
)

//go:embed agent.md
var agentMarkdown []byte

const agentName = "createos"

// installAgent writes the embedded agent markdown to ~/.opencode/agents/createos.md.
func installAgent() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("could not determine home directory: %w", err)
	}

	agentsDir := filepath.Join(home, ".opencode", "agents")
	if err := os.MkdirAll(agentsDir, 0750); err != nil {
		return fmt.Errorf("could not create agents directory: %w", err)
	}

	agentPath := filepath.Join(agentsDir, agentName+".md")
	if err := os.WriteFile(agentPath, agentMarkdown, 0600); err != nil {
		return fmt.Errorf("could not write agent file: %w", err)
	}
	return nil
}

// NewAskCommand returns the ask AI assistant command.
func NewAskCommand() *cli.Command {
	return &cli.Command{
		Name:      "ask",
		Usage:     "Ask the AI assistant to help manage your infrastructure",
		ArgsUsage: "[message]",
		Description: "Opens the OpenCode AI assistant pre-configured to use the createos CLI.\n\n" +
			"   Interactive mode (TUI):\n" +
			"     createos ask\n\n" +
			"   Non-interactive mode:\n" +
			"     createos ask \"list my VMs\"\n" +
			"     createos ask \"deploy a VM in nyc3\"\n\n" +
			"   Requires opencode to be installed: https://opencode.ai",
		Action: func(c *cli.Context) error {
			opencodeBin, err := exec.LookPath("opencode")
			if err != nil {
				if !terminal.IsInteractive() {
					return fmt.Errorf("opencode is not installed\n\n  The 'ask' command uses OpenCode (https://opencode.ai), an open-source AI coding\n  assistant, to power the CreateOS AI agent. It lets you manage your infrastructure\n  using natural language right from the terminal.\n\n  Install it with:\n    curl -fsSL https://opencode.ai/install | bash")
				}

				fmt.Println()
				pterm.Info.Println("The 'ask' command uses OpenCode (https://opencode.ai), an open-source AI coding\nassistant, to power the CreateOS AI agent. It lets you manage your infrastructure\nusing natural language right from the terminal.")
				fmt.Println()

				install, _ := pterm.DefaultInteractiveConfirm.
					WithDefaultText("opencode is not installed. Install it now?").
					WithDefaultValue(true).
					Show()
				if !install {
					return fmt.Errorf("opencode is required for the ask command\n\n  Install it manually:\n    curl -fsSL https://opencode.ai/install | bash")
				}

				pterm.Info.Println("Installing opencode...")
				installCmd := exec.CommandContext(context.Background(), "bash", "-c", "curl -fsSL https://opencode.ai/install | bash") // #nosec G204 -- install URL is hardcoded
				installCmd.Stdin = os.Stdin
				installCmd.Stdout = os.Stdout
				installCmd.Stderr = os.Stderr
				if err := installCmd.Run(); err != nil {
					return fmt.Errorf("failed to install opencode: %w", err)
				}

				opencodeBin, err = exec.LookPath("opencode")
				if err != nil {
					return fmt.Errorf("opencode was installed but not found in PATH\n\n  Try restarting your terminal and running this command again")
				}
				pterm.Success.Println("opencode installed successfully")
				fmt.Println()
			}

			if err := installAgent(); err != nil {
				return fmt.Errorf("could not install CreateOS agent: %w", err)
			}

			message := strings.Join(c.Args().Slice(), " ")

			var args []string
			if message != "" {
				args = []string{"run", message, "--agent", agentName}
			} else {
				args = []string{"--agent", agentName}
			}

			cmd := exec.CommandContext(context.Background(), opencodeBin, args...) // #nosec G204 -- opencodeBin is from exec.LookPath, args are hardcoded
			cmd.Stdin = os.Stdin
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr

			return cmd.Run()
		},
	}
}
