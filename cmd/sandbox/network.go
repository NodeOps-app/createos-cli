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

// newNetworkCommand returns the `sandbox network` group. Networks are
// private overlays that let one sandbox reach another by name or IP.
// Same shape as `sandbox disk`: create / ls / show / rm / attach / detach.
func newNetworkCommand() *cli.Command {
	return &cli.Command{
		Name:    "network",
		Aliases: []string{"net", "networks"},
		Usage:   "Manage private networks your sandboxes can talk over",
		Subcommands: []*cli.Command{
			newNetworkCreateCommand(),
			newNetworkListCommand(),
			newNetworkShowCommand(),
			newNetworkRmCommand(),
			newNetworkAttachCommand(),
			newNetworkDetachCommand(),
		},
	}
}

// ── create ───────────────────────────────────────────────────────

func newNetworkCreateCommand() *cli.Command {
	return &cli.Command{
		Name:      "create",
		Usage:     "Create a new private network",
		ArgsUsage: "[<name>]",
		Action:    runNetworkCreate,
	}
}

func runNetworkCreate(c *cli.Context) error {
	client, ok := c.App.Metadata[api.SandboxClientKey].(*api.SandboxClient)
	if !ok {
		return fmt.Errorf("you're not signed in — run 'createos login' to get started")
	}
	name := strings.TrimSpace(c.Args().First())
	if name == "" {
		if !terminal.IsInteractive() {
			return fmt.Errorf("please give the network a name\n\n  Example:\n    createos sandbox network create my-app")
		}
		v, err := pterm.DefaultInteractiveTextInput.
			WithDefaultText("Name this network (your sandboxes will reach each other on it)").
			Show()
		if err != nil {
			return fmt.Errorf("could not read name: %w", err)
		}
		name = strings.TrimSpace(v)
		if name == "" {
			fmt.Println("Cancelled. No network created.")
			return nil
		}
	}
	n, err := client.CreateNetwork(c.Context, name)
	if err != nil {
		return err
	}
	pterm.Success.Printfln("Created network %s (%s)", n.Name, n.ID)
	pterm.Println(pterm.Gray("  Attach at create time:  createos sandbox create --network " + n.Name))
	pterm.Println(pterm.Gray("  Or live-attach later:   createos sandbox network attach <sandbox> " + n.Name))
	return nil
}

// ── list ─────────────────────────────────────────────────────────

func newNetworkListCommand() *cli.Command {
	return &cli.Command{
		Name:    "ls",
		Aliases: []string{"list"},
		Usage:   "List your networks",
		Action:  runNetworkList,
	}
}

func runNetworkList(c *cli.Context) error {
	client, ok := c.App.Metadata[api.SandboxClientKey].(*api.SandboxClient)
	if !ok {
		return fmt.Errorf("you're not signed in — run 'createos login' to get started")
	}
	nets, err := client.ListNetworks(c.Context)
	if err != nil {
		return err
	}
	output.Render(c, nets, func() {
		if len(nets) == 0 {
			fmt.Println("You don't have any networks yet.")
			pterm.Println(pterm.Gray("  Create one with: createos sandbox network create <name>"))
			return
		}
		table := pterm.TableData{{"Name", "ID", "Sandboxes", "Created"}}
		for _, n := range nets {
			table = append(table, []string{
				n.Name, n.ID,
				fmt.Sprintf("%d", n.MemberCount),
				n.CreatedAt.Format("2006-01-02 15:04"),
			})
		}
		_ = pterm.DefaultTable.WithHasHeader().WithData(table).Render() //nolint:errcheck
	})
	return nil
}

// ── show ─────────────────────────────────────────────────────────

func newNetworkShowCommand() *cli.Command {
	return &cli.Command{
		Name:      "show",
		Usage:     "Show one network's details (including attached sandboxes)",
		ArgsUsage: "[<name|id>]",
		Action:    runNetworkShow,
	}
}

func runNetworkShow(c *cli.Context) error {
	client, ok := c.App.Metadata[api.SandboxClientKey].(*api.SandboxClient)
	if !ok {
		return fmt.Errorf("you're not signed in — run 'createos login' to get started")
	}
	ref := strings.TrimSpace(c.Args().First())
	if ref == "" {
		if !terminal.IsInteractive() {
			return fmt.Errorf("please provide a network name or ID\n\n  To see your networks, run:\n    createos sandbox network ls")
		}
		picked, err := pickNetwork(c, client, "Show which network?")
		if err != nil {
			return err
		}
		if picked == "" {
			fmt.Println("Cancelled. Nothing to show.")
			return nil
		}
		ref = picked
	}
	n, err := client.GetNetwork(c.Context, ref)
	if err != nil {
		return err
	}
	output.Render(c, n, func() {
		label := pterm.NewStyle(pterm.FgCyan)
		row := func(k, v string) {
			if v == "" {
				return
			}
			label.Printf("  %-12s ", k+":")
			fmt.Println(v)
		}
		pterm.Println()
		pterm.NewStyle(pterm.FgCyan, pterm.Bold).Printfln("  %s (%s)", n.Name, n.ID)
		pterm.Println()
		row("Created", n.CreatedAt.Format("2006-01-02 15:04:05"))
		row("Sandboxes", fmt.Sprintf("%d", n.MemberCount))

		if len(n.Members) > 0 {
			pterm.Println()
			pterm.Println(pterm.Gray("  Attached sandboxes:"))
			table := make(pterm.TableData, 0, 1+len(n.Members))
			table = append(table, []string{"Sandbox", "Name", "Status", "IP", "Reachable as"})
			for _, m := range n.Members {
				reachable := m.SandboxID
				if m.Name != "" {
					reachable = m.Name + "." + n.Name + ".fc.local"
				}
				table = append(table, []string{m.SandboxID, m.Name, m.Status, m.IP, reachable})
			}
			_ = pterm.DefaultTable.WithHasHeader().WithData(table).Render() //nolint:errcheck
			pterm.Println(pterm.Gray("  Tip: inside any of these sandboxes you can `ping <name>` or curl by name."))
		}
	})
	return nil
}

// ── rm ───────────────────────────────────────────────────────────

func newNetworkRmCommand() *cli.Command {
	return &cli.Command{
		Name:      "rm",
		Aliases:   []string{"delete"},
		Usage:     "Delete one or more networks (each must have no live members)",
		ArgsUsage: "[<name|id> …]",
		Flags: []cli.Flag{
			&cli.BoolFlag{Name: "yes", Aliases: []string{"y", "force"}, Usage: "Skip the confirmation prompt"},
		},
		Action: runNetworkRm,
	}
}

func runNetworkRm(c *cli.Context) error {
	client, ok := c.App.Metadata[api.SandboxClientKey].(*api.SandboxClient)
	if !ok {
		return fmt.Errorf("you're not signed in — run 'createos login' to get started")
	}
	// Collect refs from positionals; accept --yes/-y/--force after positionals too.
	refs, forceFromArgs := splitForceFlag(c.Args().Slice())
	if len(refs) == 0 {
		if !terminal.IsInteractive() {
			return fmt.Errorf("please provide at least one network name or ID")
		}
		picked, err := pickNetworksForDelete(c, client)
		if err != nil {
			return err
		}
		if len(picked) == 0 {
			fmt.Println("Cancelled.")
			return nil
		}
		refs = picked
	}
	force := c.Bool("yes") || forceFromArgs
	if !terminal.IsInteractive() && !force {
		return fmt.Errorf("non-interactive: pass --yes to confirm deletion")
	}
	if terminal.IsInteractive() && !force {
		prompt := fmt.Sprintf("Permanently delete network %q?", refs[0])
		if len(refs) > 1 {
			prompt = fmt.Sprintf("Permanently delete %d networks?", len(refs))
		}
		ok, err := pterm.DefaultInteractiveConfirm.
			WithDefaultText(prompt).
			WithDefaultValue(false).
			Show()
		if err != nil {
			return fmt.Errorf("could not read confirmation: %w", err)
		}
		if !ok {
			fmt.Println("Cancelled.")
			return nil
		}
	}
	failed := 0
	for _, ref := range refs {
		if err := deleteNetworkCascade(c, client, ref); err != nil {
			pterm.Error.Printfln("%s: %v", ref, err)
			failed++
			continue
		}
		pterm.Success.Printfln("Deleted network %s", ref)
	}
	if failed > 0 {
		return fmt.Errorf("%d of %d deletes failed", failed, len(refs))
	}
	return nil
}

// deleteNetworkCascade detaches any attached sandboxes, then deletes
// the network. "rm" means "remove it, including the wiring" — users
// shouldn't have to detach manually first.
func deleteNetworkCascade(c *cli.Context, client *api.SandboxClient, ref string) error {
	n, err := client.GetNetwork(c.Context, ref)
	if err != nil {
		return err
	}
	for _, m := range n.Members {
		if m.SandboxID == "" {
			continue
		}
		if derr := client.DetachNetwork(c.Context, m.SandboxID, n.ID); derr != nil {
			return fmt.Errorf("detach %s: %w", m.SandboxID, derr)
		}
		pterm.Println(pterm.Gray(fmt.Sprintf("  detached %s from %s", m.SandboxID, n.Name)))
	}
	return client.DeleteNetwork(c.Context, ref)
}

// pickNetworksForDelete renders a multi-select over the caller's networks
// and returns the picked names. Returns nil/empty when the user cancels.
func pickNetworksForDelete(c *cli.Context, client *api.SandboxClient) ([]string, error) {
	nets, err := client.ListNetworks(c.Context)
	if err != nil {
		return nil, err
	}
	if len(nets) == 0 {
		fmt.Println("You don't have any networks to delete.")
		return nil, nil
	}
	options := make([]string, 0, len(nets))
	byOpt := make(map[string]string, len(nets))
	for _, n := range nets {
		opt := fmt.Sprintf("%s   (sandboxes: %d, id: %s)", n.Name, n.MemberCount, n.ID)
		options = append(options, opt)
		byOpt[opt] = n.Name
	}
	picked, err := multiselect("Pick networks to delete (space = select, enter = confirm)").
		WithOptions(options).
		Show()
	if err != nil {
		return nil, fmt.Errorf("could not read your selection: %w", err)
	}
	out := make([]string, 0, len(picked))
	for _, p := range picked {
		if name, ok := byOpt[p]; ok {
			out = append(out, name)
		}
	}
	return out, nil
}

// ── attach ───────────────────────────────────────────────────────

func newNetworkAttachCommand() *cli.Command {
	return &cli.Command{
		Name:      "attach",
		Usage:     "Add a sandbox to a network",
		ArgsUsage: "[<sandbox> <network>]",
		Action:    runNetworkAttach,
	}
}

func runNetworkAttach(c *cli.Context) error {
	client, ok := c.App.Metadata[api.SandboxClientKey].(*api.SandboxClient)
	if !ok {
		return fmt.Errorf("you're not signed in — run 'createos login' to get started")
	}
	args := c.Args().Slice()
	sandboxRef, netRef := "", ""
	if len(args) > 0 {
		sandboxRef = args[0]
	}
	if len(args) > 1 {
		netRef = args[1]
	}
	tty := terminal.IsInteractive()
	if sandboxRef == "" {
		if !tty {
			return fmt.Errorf("usage: createos sandbox network attach <sandbox> <network>")
		}
		pickedID, label, err := pickByStatus(c, client, "Attach which sandbox?", "running")
		if err != nil {
			return err
		}
		if pickedID == "" {
			fmt.Println("Cancelled.")
			return nil
		}
		sandboxRef = label
	}
	sandboxID, err := resolveSandboxRef(c.Context, client, sandboxRef)
	if err != nil {
		return err
	}
	if netRef == "" {
		if !tty {
			return fmt.Errorf("usage: createos sandbox network attach <sandbox> <network>")
		}
		picked, err := pickNetwork(c, client, "Attach to which network?")
		if err != nil {
			return err
		}
		if picked == "" {
			fmt.Println("Cancelled.")
			return nil
		}
		netRef = picked
	}
	if err := client.AttachNetwork(c.Context, sandboxID, netRef); err != nil {
		return err
	}
	pterm.Success.Printfln("Attached %s → network %s", refLabel(sandboxRef, sandboxID), netRef)
	pterm.Println(pterm.Gray("  Other sandboxes on this network can now reach this one by name."))
	return nil
}

// ── detach ───────────────────────────────────────────────────────

func newNetworkDetachCommand() *cli.Command {
	return &cli.Command{
		Name:      "detach",
		Usage:     "Remove a sandbox from a network",
		ArgsUsage: "[<sandbox> <network>]",
		Flags: []cli.Flag{
			&cli.BoolFlag{Name: "yes", Aliases: []string{"y"}, Usage: "Skip the confirmation prompt"},
		},
		Action: runNetworkDetach,
	}
}

func runNetworkDetach(c *cli.Context) error {
	client, ok := c.App.Metadata[api.SandboxClientKey].(*api.SandboxClient)
	if !ok {
		return fmt.Errorf("you're not signed in — run 'createos login' to get started")
	}
	args := c.Args().Slice()
	sandboxRef, netRef := "", ""
	if len(args) > 0 {
		sandboxRef = args[0]
	}
	if len(args) > 1 {
		netRef = args[1]
	}
	tty := terminal.IsInteractive()
	if sandboxRef == "" {
		if !tty {
			return fmt.Errorf("usage: createos sandbox network detach <sandbox> <network>")
		}
		pickedID, label, err := pickByStatus(c, client, "Detach from which sandbox?", "running")
		if err != nil {
			return err
		}
		if pickedID == "" {
			fmt.Println("Cancelled.")
			return nil
		}
		sandboxRef = label
	}
	sandboxID, err := resolveSandboxRef(c.Context, client, sandboxRef)
	if err != nil {
		return err
	}
	if netRef == "" {
		if !tty {
			return fmt.Errorf("usage: createos sandbox network detach <sandbox> <network>")
		}
		picked, err := pickNetwork(c, client, "Detach from which network?")
		if err != nil {
			return err
		}
		if picked == "" {
			fmt.Println("Cancelled.")
			return nil
		}
		netRef = picked
	}
	force := c.Bool("yes")
	if !tty && !force {
		return fmt.Errorf("non-interactive: pass --yes to confirm detach")
	}
	if tty && !force {
		ok, err := pterm.DefaultInteractiveConfirm.
			WithDefaultText(fmt.Sprintf("Remove %s from network %s?", refLabel(sandboxRef, sandboxID), netRef)).
			WithDefaultValue(false).
			Show()
		if err != nil {
			return fmt.Errorf("could not read confirmation: %w", err)
		}
		if !ok {
			fmt.Println("Cancelled.")
			return nil
		}
	}
	if err := client.DetachNetwork(c.Context, sandboxID, netRef); err != nil {
		return err
	}
	pterm.Success.Printfln("Detached %s from network %s", refLabel(sandboxRef, sandboxID), netRef)
	return nil
}

// pickNetwork renders a single-select picker over the caller's networks
// and returns the picked NAME (the server accepts it wherever an ID
// works). Returns "" when the user cancels.
func pickNetwork(c *cli.Context, client *api.SandboxClient, title string) (string, error) {
	nets, err := client.ListNetworks(c.Context)
	if err != nil {
		return "", err
	}
	if len(nets) == 0 {
		fmt.Println("You don't have any networks yet.")
		pterm.Println(pterm.Gray("  Create one with: createos sandbox network create <name>"))
		return "", nil
	}
	options := make([]string, 0, len(nets))
	byOpt := make(map[string]string, len(nets))
	for _, n := range nets {
		opt := fmt.Sprintf("%s   (sandboxes: %d, id: %s)", n.Name, n.MemberCount, n.ID)
		options = append(options, opt)
		byOpt[opt] = n.Name
	}
	picked, err := pterm.DefaultInteractiveSelect.
		WithOptions(options).
		WithDefaultText(title).
		Show()
	if err != nil {
		return "", fmt.Errorf("could not read your selection: %w", err)
	}
	return byOpt[picked], nil
}
