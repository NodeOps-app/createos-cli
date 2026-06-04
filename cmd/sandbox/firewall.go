package sandbox

import (
	"fmt"
	"strings"

	"github.com/pterm/pterm"
	"github.com/urfave/cli/v2"

	"github.com/NodeOps-app/createos-cli/internal/api"
	"github.com/NodeOps-app/createos-cli/internal/output"
	"github.com/NodeOps-app/createos-cli/internal/terminal"
)

// newFirewallCommand wires up `createos sandbox firewall`.
//
// Wraps the server's egress-rules endpoint with friendlier
// terminology. "firewall show / set / clear" reads more naturally to
// non-network engineers than "egress allowlist".
func newFirewallCommand() *cli.Command {
	return &cli.Command{
		Name:    "firewall",
		Aliases: []string{"fw"},
		Usage:   "Control what a sandbox can reach on the internet",
		Subcommands: []*cli.Command{
			newFirewallShowCommand(),
			newFirewallSetCommand(),
			newFirewallClearCommand(),
		},
	}
}

func newFirewallShowCommand() *cli.Command {
	return &cli.Command{
		Name:      "show",
		Usage:     "Show what sites/IPs the sandbox is allowed to reach",
		ArgsUsage: "[<sandbox>]",
		Action:    runFirewallShow,
	}
}

func runFirewallShow(c *cli.Context) error {
	client, id, _, err := requireRunningSandbox(c, "Show firewall for which sandbox?")
	if err != nil || id == "" {
		return err
	}
	rules, err := client.GetEgress(c.Context, id)
	if err != nil {
		return err
	}
	output.Render(c, struct {
		Rules []string `json:"rules"`
	}{rules}, func() {
		if len(rules) == 0 {
			pterm.Success.Println("Firewall is open — all outbound traffic is allowed.")
			pterm.Println(pterm.Gray("  Lock it down with:  createos sandbox firewall set <sandbox> example.com 1.1.1.1"))
			return
		}
		pterm.Println()
		pterm.NewStyle(pterm.FgCyan, pterm.Bold).Println("  Allowed outbound destinations:")
		for _, r := range rules {
			pterm.Printfln("    • %s", r)
		}
		pterm.Println()
		pterm.Println(pterm.Gray("  Everything else is blocked at the host firewall."))
	})
	return nil
}

func newFirewallSetCommand() *cli.Command {
	return &cli.Command{
		Name:      "set",
		Usage:     "Replace the allowlist with a new set of hosts / IPs",
		ArgsUsage: "<sandbox> <host-or-ip> [<host-or-ip>…]",
		Description: `Lock the sandbox's outbound traffic to the given destinations.
Rules can be:
  • a DNS name      (pypi.org, github.com)
  • a host:port     (1.1.1.1:53)
  • an IP literal   (8.8.8.8)

Examples:
  createos sandbox firewall set my-box pypi.org github.com
  createos sandbox firewall set my-box 1.1.1.1:53 example.com`,
		Action: runFirewallSet,
	}
}

func runFirewallSet(c *cli.Context) error {
	client, ok := c.App.Metadata[api.SandboxClientKey].(*api.SandboxClient)
	if !ok {
		return fmt.Errorf("you're not signed in — run 'createos login' to get started")
	}
	args := c.Args().Slice()
	if len(args) < 2 {
		if !terminal.IsInteractive() {
			return fmt.Errorf("usage: createos sandbox firewall set <sandbox> <host-or-ip> [<host-or-ip>…]")
		}
		// Sandbox picker, then prompt for rules — pre-filled with the
		// current allowlist so "edit" works (add, remove, or tweak)
		// instead of forcing the user to retype everything.
		pickedID, label, err := pickByStatus(c, client, "Set firewall on which sandbox?", "running")
		if err != nil {
			return err
		}
		if pickedID == "" {
			fmt.Println("Cancelled.")
			return nil
		}
		current, _ := client.GetEgress(c.Context, pickedID)
		prefill := strings.Join(current, ", ")
		if len(current) == 0 {
			pterm.Println(pterm.Gray("  Firewall is currently open. Enter destinations to lock it down, or leave empty to cancel."))
		} else {
			pterm.Println(pterm.Gray("  Edit the current rules below. Clear to leave the firewall open (use 'firewall clear' instead)."))
		}
		v, err := pterm.DefaultInteractiveTextInput.
			WithDefaultText("Allowed destinations (comma-separated)").
			WithDefaultValue(prefill).
			Show()
		if err != nil {
			return fmt.Errorf("could not read rules: %w", err)
		}
		rules := splitRules(v)
		if len(rules) == 0 {
			fmt.Println("Cancelled. Rules unchanged.")
			return nil
		}
		return applyFirewall(c, client, pickedID, label, rules)
	}
	sandboxRef, raw := args[0], args[1:]
	id, err := resolveSandboxRef(c.Context, client, sandboxRef)
	if err != nil {
		return err
	}
	rules := make([]string, 0, len(raw))
	for _, r := range raw {
		rules = append(rules, splitRules(r)...)
	}
	return applyFirewall(c, client, id, sandboxRef, rules)
}

func newFirewallClearCommand() *cli.Command {
	return &cli.Command{
		Name:      "clear",
		Usage:     "Open the firewall — let the sandbox reach anywhere on the internet",
		ArgsUsage: "[<sandbox>]",
		Flags: []cli.Flag{
			&cli.BoolFlag{Name: "yes", Aliases: []string{"y", "force"}, Usage: "Skip the confirmation prompt"},
		},
		Action: runFirewallClear,
	}
}

func runFirewallClear(c *cli.Context) error {
	client, id, ref, err := requireRunningSandbox(c, "Clear firewall on which sandbox?")
	if err != nil || id == "" {
		return err
	}
	force := c.Bool("yes")
	if terminal.IsInteractive() && !force {
		ok, perr := pterm.DefaultInteractiveConfirm.
			WithDefaultText(fmt.Sprintf("Open firewall on %s? All outbound traffic will be allowed.", refLabel(ref, id))).
			WithDefaultValue(false).
			Show()
		if perr != nil {
			return fmt.Errorf("could not read confirmation: %w", perr)
		}
		if !ok {
			fmt.Println("Cancelled.")
			return nil
		}
	}
	if _, err := client.SetEgress(c.Context, id, []string{}); err != nil {
		return err
	}
	pterm.Success.Printfln("Firewall cleared on %s — all outbound traffic allowed.", refLabel(ref, id))
	return nil
}

// applyFirewall PUTs the rules and reports the new state.
func applyFirewall(c *cli.Context, client *api.SandboxClient, id, ref string, rules []string) error {
	stored, err := client.SetEgress(c.Context, id, rules)
	if err != nil {
		return err
	}
	pterm.Success.Printfln("Firewall updated on %s — %d rule(s) active.", refLabel(ref, id), len(stored))
	for _, r := range stored {
		pterm.Println(pterm.Gray("  • " + r))
	}
	return nil
}

// splitRules splits a comma- or whitespace-separated string into trimmed
// non-empty tokens.
func splitRules(s string) []string {
	fields := strings.FieldsFunc(s, func(r rune) bool {
		return r == ',' || r == ' ' || r == '\t' || r == '\n'
	})
	out := make([]string, 0, len(fields))
	for _, f := range fields {
		if t := strings.TrimSpace(f); t != "" {
			out = append(out, t)
		}
	}
	return out
}

// requireRunningSandbox resolves the first positional arg (or prompts
// on TTY) into a sandbox id. Returns id="" and nil error on cancel.
func requireRunningSandbox(c *cli.Context, prompt string) (*api.SandboxClient, string, string, error) {
	client, ok := c.App.Metadata[api.SandboxClientKey].(*api.SandboxClient)
	if !ok {
		return nil, "", "", fmt.Errorf("you're not signed in — run 'createos login' to get started")
	}
	ref := strings.TrimSpace(c.Args().First())
	if ref == "" {
		if !terminal.IsInteractive() {
			return client, "", "", fmt.Errorf("please provide a sandbox ID or name")
		}
		pickedID, label, err := pickByStatus(c, client, prompt, "running")
		if err != nil {
			return client, "", "", err
		}
		if pickedID == "" {
			fmt.Println("Cancelled.")
			return client, "", "", nil
		}
		return client, pickedID, label, nil
	}
	id, err := resolveSandboxRef(c.Context, client, ref)
	if err != nil {
		return client, "", "", err
	}
	return client, id, ref, nil
}
