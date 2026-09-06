package sandbox

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/pterm/pterm"
	"github.com/urfave/cli/v2"
	"golang.org/x/crypto/ssh"

	"github.com/NodeOps-app/createos-cli/internal/api"
	"github.com/NodeOps-app/createos-cli/internal/terminal"
)

// These values end up spliced into a ProxyCommand shell line, so
// anything permissive becomes a local-code-exec bug.
var (
	editorUserRE  = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_-]{0,31}$`)
	editorHostRE  = regexp.MustCompile(`^[a-zA-Z0-9._-]{1,253}$`)
	editorAliasRE = regexp.MustCompile(`^sb-[0-9a-z]{26}$`)
	// editorNameRE gates a sandbox's friendly name before it lands on the
	// `Host <id> <name>` line. SSH's Host aliases forbid whitespace, `?`,
	// and `*`; we further restrict to plain identifier chars so a mutated
	// name can't smuggle newlines / control bytes.
	editorNameRE = regexp.MustCompile(`^[a-zA-Z0-9._-]{1,64}$`)
)

// One block per sandbox id in ~/.ssh/config; re-runs rewrite in place.
const (
	sshConfigBlockBegin = "# BEGIN createos %s"
	sshConfigBlockEnd   = "# END createos %s"
)

// editorMinMemMiB is the smallest sandbox we allow remote-editor connections
// on. Zed's remote-server and VS Code's Remote-SSH easily eat >1 GiB just
// hosting the language server; on the 1 GiB shapes the sandbox OOMs mid-
// session. Set to 2049 = strictly larger than 2 GiB (the pool default).
const editorMinMemMiB = 2049

func newEditorCommand() *cli.Command {
	return &cli.Command{
		Name:                   "editor",
		Usage:                  "Connect a remote editor (Zed, Cursor, VS Code) to a sandbox",
		ArgsUsage:              "[<sandbox>]",
		UseShortOptionHandling: true,
		Description: `Wire up a sandbox for remote-development in one command:

  - ensures your local SSH key is registered on the sandbox
  - starts sshd inside the sandbox (via devbox:1's openssh-server)
  - writes an entry into ~/.ssh/config so plain 'ssh <alias>' works
  - launches your editor with the remote pre-selected

Two transports:

  --via tunnel  (default when VPN is down)
    SSH through the gateway using OpenSSH ProxyJump. No background
    processes. Works anywhere.

  --via vpn     (default when the VPN is already up)
    Direct connection to the sandbox's overlay IP via the CreateOS
    WireGuard tunnel. Full network access, not just SSH.

The command is interactive by default — pass all three flags plus --yes
to run without prompts.

Examples:
  createos sandbox editor                       # pick sandbox + mode + editor
  createos sandbox editor my-box                # interactive on named box
  createos sandbox editor my-box --via tunnel --editor zed --yes
  createos sandbox editor --remove my-box       # remove the SSH config entry

Note: flag options like --via and --remove must appear BEFORE the
sandbox positional (urfave/cli stops flag parsing at the first
positional).`,
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:  "via",
				Usage: "Transport: 'tunnel' (SSH via gateway) or 'vpn' (direct via CreateOS VPN). Auto-picks based on VPN state when omitted.",
			},
			&cli.StringFlag{
				Name:  "editor",
				Usage: "Editor to launch after connect: 'zed', 'cursor', 'code' (VS Code), or 'none' (config only)",
			},
			&cli.StringFlag{
				Name:    "user",
				Aliases: []string{"u"},
				Value:   "root",
				Usage:   "Username inside the sandbox",
			},
			&cli.BoolFlag{
				Name:    "yes",
				Aliases: []string{"y"},
				Usage:   "Skip prompts; accept smart defaults (key upload, editor auto-detect, etc.)",
			},
			&cli.BoolFlag{
				Name:  "no-launch",
				Usage: "Wire up SSH config but don't launch the editor",
			},
			&cli.BoolFlag{
				Name:  "remove",
				Usage: "Remove this sandbox's block from ~/.ssh/config and exit",
			},
			&cli.BoolFlag{
				Name:  "no-sweep",
				Usage: "Skip auto-cleanup of ~/.ssh/config blocks for sandboxes that no longer exist",
			},
		},
		Action: runEditor,
	}
}

func runEditor(c *cli.Context) error {
	client, ok := c.App.Metadata[api.SandboxClientKey].(*api.SandboxClient)
	if !ok {
		return fmt.Errorf("you're not signed in — run 'createos login' first")
	}

	// Resolve sandbox — mirror the shell command's picker so the UX
	// is consistent (interactive picker on no-arg, name-or-id lookup
	// otherwise).
	ref := strings.TrimSpace(c.Args().First())
	var id string
	if ref == "" {
		if !terminal.IsInteractive() {
			return fmt.Errorf("please provide a sandbox ID or name\n\n  Example:\n    createos sandbox editor my-box")
		}
		pickedID, label, err := pickByStatus(c, client, "Edit which sandbox?", api.SandboxStatusRunning)
		if err != nil {
			return err
		}
		if pickedID == "" {
			fmt.Println("Cancelled. Nothing happened.")
			return nil
		}
		id, ref = pickedID, label
	} else {
		resolved, err := resolveSandboxRef(c.Context, client, ref)
		if err != nil {
			return err
		}
		id = resolved
	}

	alias := sshAlias(id)
	if !editorAliasRE.MatchString(alias) {
		return fmt.Errorf("refusing to write shell-unsafe SSH alias %q", alias)
	}

	// --remove short-circuits: yank our block + delete our dedicated key.
	if c.Bool("remove") {
		removed, ferr := removeSSHBlock(alias)
		if ferr != nil {
			return ferr
		}
		keyRemoved := removeDedicatedKey(alias)
		switch {
		case removed && keyRemoved:
			pterm.Success.Printfln("removed ~/.ssh/config entry + local key for %s", alias)
		case removed:
			pterm.Success.Printfln("removed ~/.ssh/config entry: %s", alias)
		case keyRemoved:
			pterm.Success.Printfln("removed local key for %s", alias)
		default:
			pterm.Info.Printfln("nothing to remove for %s", alias)
		}
		return nil
	}

	// --- 1. Pick transport mode ---------------------------------------------
	mode, err := chooseMode(c)
	if err != nil {
		return err
	}

	// --- 2. Ensure a dedicated per-sandbox keypair --------------------------
	// Every sandbox gets its own throwaway keypair — the user's ~/.ssh/
	// is never touched.
	privPath, pubBytes, generated, err := ensureDedicatedKey(alias)
	if err != nil {
		return err
	}
	if generated {
		pterm.Info.Printfln("generated a fresh SSH key for this sandbox: %s", privPath)
	}

	// --- 3. Load sandbox row + verify it's running --------------------------
	sb, err := client.GetSandbox(c.Context, id)
	if err != nil {
		return fmt.Errorf("could not fetch sandbox %s: %w", ref, err)
	}
	if sb.Status != api.SandboxStatusRunning {
		return fmt.Errorf("sandbox %s is %s — resume or wait for it to be running", ref, sb.Status)
	}
	if sb.MemMib < editorMinMemMiB {
		return fmt.Errorf("sandbox %s has %d MiB memory — remote editors need at least %d MiB (use a shape >2 GiB, e.g. s-4vcpu-4gb)",
			ref, sb.MemMib, editorMinMemMiB)
	}
	sbIP := ""
	if sb.IP != nil {
		sbIP = strings.TrimSpace(*sb.IP)
	}
	if mode == "vpn" && sbIP == "" {
		return fmt.Errorf("sandbox %s has no overlay IP yet — try --via tunnel", ref)
	}

	user := strings.TrimSpace(c.String("user"))
	if user == "" {
		user = "root"
	}
	if !editorUserRE.MatchString(user) {
		return fmt.Errorf("refusing shell-unsafe --user %q (allowed: %s)", user, editorUserRE)
	}

	// --- 4. VPN preflight ---------------------------------------------------
	if mode == "vpn" {
		if perr := preflightVPN(c, client, sb.ID); perr != nil {
			return perr
		}
	}

	// --- 5. Register our dedicated pubkey --------
	// Gateway auths against sandboxes.ssh_pubkeys (DB); guest sshd reads
	// /root/.ssh/authorized_keys. Both hops in the tunnel path need it.
	sp, _ := pterm.DefaultSpinner.WithText("Registering our SSH key on the sandbox…").Start() //nolint:errcheck // spinner init failure is benign UI-only
	if _, addErr := client.AddSSHPubkeys(c.Context, id, []string{strings.TrimSpace(string(pubBytes))}); addErr != nil {
		sp.Fail(fmt.Sprintf("could not register key with gateway: %v", addErr))
		return addErr
	}
	if authErr := ensureAuthorizedKey(c, client, id, user, ref, pubBytes, true); authErr != nil {
		sp.Fail(fmt.Sprintf("could not install key in guest: %v", authErr))
		return authErr
	}
	sp.Success("SSH key registered on sandbox")

	sp, _ = pterm.DefaultSpinner.WithText("Starting sshd inside the sandbox…").Start() //nolint:errcheck // spinner init failure is benign UI-only
	if sshdErr := startGuestSshd(c, client, id, user); sshdErr != nil {
		sp.Fail(sshdErr.Error())
		return sshdErr
	}
	sp.Success("sshd running in the sandbox")

	// Sweep other ~/.ssh/config blocks that reference sandboxes the user
	// no longer owns / that no longer exist. Runs before we write the
	// fresh block so the atomic tmp-rewrite flushes the pruning too.
	if !c.Bool("no-sweep") {
		if pruned, serr := sweepStaleBlocks(c.Context, client, alias); serr == nil && len(pruned) > 0 {
			pterm.Info.Printfln("cleaned up %d stale entr%s: %s",
				len(pruned), plural(len(pruned), "y", "ies"), strings.Join(pruned, ", "))
		}
	}

	// --- 6. Write the ~/.ssh/config block -----------------------------------
	gwHost, gwPort := gatewayAddr()
	if !editorHostRE.MatchString(gwHost) {
		return fmt.Errorf("refusing shell-unsafe gateway host %q", gwHost)
	}
	sbName := ""
	if sb.Name != nil {
		sbName = *sb.Name
	}
	block, err := renderSSHBlock(alias, mode, id, sbIP, gwHost, gwPort, user, privPath, sbName)
	if err != nil {
		return err
	}
	if writeErr := writeSSHBlock(alias, block); writeErr != nil {
		return writeErr
	}
	pterm.Success.Printfln("~/.ssh/config: entry %s (%s)", alias, mode)

	// --- 7. Wait for :22 to be reachable -------------------------------------
	sp, _ = pterm.DefaultSpinner.WithText("Waiting for sshd to accept connections…").Start() //nolint:errcheck // spinner init failure is benign UI-only
	probeCtx, cancel := context.WithTimeout(c.Context, 15*time.Second)
	defer cancel()
	if probeErr := probeSSH(probeCtx, alias, 30*time.Second); probeErr != nil {
		sp.Warning("sshd didn't answer in 30 s — connection may still work; try `ssh " + alias + "` yourself.")
	} else {
		sp.Success("sshd is answering")
	}

	// --- 8. Launch editor ----------------------------------------------------
	if c.Bool("no-launch") {
		printFollowup(alias, sbName)
		return nil
	}
	choice, err := chooseEditor(c)
	if err != nil {
		return err
	}
	if choice == "none" {
		printFollowup(alias, sbName)
		return nil
	}
	if err := launchEditor(choice, alias, sbName); err != nil {
		pterm.Warning.Printfln("could not launch %s: %v", choice, err)
		printFollowup(alias, sbName)
		return nil
	}
	pterm.Success.Printfln("launched %s", choice)
	return nil
}

func chooseMode(c *cli.Context) (string, error) {
	if v := strings.ToLower(strings.TrimSpace(c.String("via"))); v != "" {
		if v != "tunnel" && v != "vpn" {
			return "", fmt.Errorf("--via must be 'tunnel' or 'vpn', got %q", v)
		}
		return v, nil
	}
	def := "tunnel"
	if isVPNUp() {
		def = "vpn"
	}
	if c.Bool("yes") || !terminal.IsInteractive() {
		return def, nil
	}
	opts := []string{"tunnel — SSH via gateway", "vpn — direct to the sandbox (full network access)"}
	sel, err := pterm.DefaultInteractiveSelect.
		WithOptions(opts).
		WithDefaultText("Connection mode").
		WithDefaultOption(opts[0]).
		Show()
	if err != nil {
		return "", err
	}
	if strings.HasPrefix(sel, "vpn") {
		return "vpn", nil
	}
	return "tunnel", nil
}

func chooseEditor(c *cli.Context) (string, error) {
	if v := strings.ToLower(strings.TrimSpace(c.String("editor"))); v != "" {
		return v, nil
	}
	installed := detectEditors()
	if len(installed) == 0 {
		pterm.Warning.Println("no supported editor found on PATH (zed / cursor / code)")
		return "none", nil
	}
	if c.Bool("yes") || !terminal.IsInteractive() {
		return installed[0], nil
	}
	// Copy first so we don't mutate detectEditors' return slice on the
	// off chance it's cached elsewhere.
	opts := make([]string, 0, len(installed)+1)
	opts = append(opts, installed...)
	opts = append(opts, "none")
	sel, err := pterm.DefaultInteractiveSelect.
		WithOptions(opts).
		WithDefaultText("Editor to launch").
		WithDefaultOption(opts[0]).
		Show()
	if err != nil {
		return "", err
	}
	return sel, nil
}

func detectEditors() []string {
	out := []string{}
	for _, e := range []string{"zed", "cursor", "code"} {
		if _, err := exec.LookPath(e); err == nil {
			out = append(out, e)
		}
	}
	return out
}

func sshAlias(id string) string { return id }

// keysDir keeps per-sandbox keys under ~/.config/createos so the user's
// ~/.ssh stays untouched.
func keysDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve $HOME: %w", err)
	}
	return filepath.Join(home, ".config", "createos", "keys"), nil
}

// ensureMuxDir makes the directory ControlPath's %n-keyed sockets live in.
//
// Every tunnel-mode sandbox shares HostName 127.0.0.1 + User root, and VPN
// mode reuses an overlay IP once its old owner is destroyed — so a
// ControlPath built from %h/%r/%p (a common personal ~/.ssh/config default)
// collides across sandboxes. `ssh <alias>` then reuses another sandbox's
// stale multiplexed connection instead of opening one to the box actually
// asked for, and hangs or times out against a box that no longer exists.
// %n is the alias itself — the one token guaranteed unique per sandbox,
// which is why renderSSHBlock pins ControlPath here instead of trusting
// whatever the user's own ssh config already has for `Host *`.
func ensureMuxDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve $HOME: %w", err)
	}
	dir := filepath.Join(home, ".config", "createos", "mux")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("mkdir mux dir: %w", err)
	}
	return dir, nil
}

func dedicatedKeyPath(alias string) (string, string, error) {
	dir, err := keysDir()
	if err != nil {
		return "", "", err
	}
	priv := filepath.Join(dir, alias)
	return priv, priv + ".pub", nil
}

// ensureDedicatedKey generates an ed25519 keypair for the sandbox on
// first call and reuses it after. Returns (privPath, pubBytes,
// generatedThisCall). Key is unprotected — it's single-sandbox scope
// and lives under 0700/0600 modes.
func ensureDedicatedKey(alias string) (privPath string, pubBytes []byte, generated bool, err error) {
	priv, pub, err := dedicatedKeyPath(alias)
	if err != nil {
		return "", nil, false, err
	}
	if b, rerr := os.ReadFile(pub); rerr == nil { // #nosec G304 -- our own generated pubkey
		return priv, b, false, nil
	}
	if err := os.MkdirAll(filepath.Dir(priv), 0o700); err != nil {
		return "", nil, false, fmt.Errorf("mkdir keys dir: %w", err)
	}
	// Generate ed25519.
	pubKey, privKey, kerr := ed25519.GenerateKey(rand.Reader)
	if kerr != nil {
		return "", nil, false, fmt.Errorf("generate ed25519 key: %w", kerr)
	}
	// Marshal PRIVATE key in OpenSSH format.
	pemBlock, merr := ssh.MarshalPrivateKey(privKey, "createos-cli sandbox editor")
	if merr != nil {
		return "", nil, false, fmt.Errorf("marshal private key: %w", merr)
	}
	if werr := os.WriteFile(priv, pem.EncodeToMemory(pemBlock), 0o600); werr != nil {
		return "", nil, false, fmt.Errorf("write %s: %w", priv, werr)
	}
	// Marshal PUBLIC key in authorized_keys format.
	sshPub, perr := ssh.NewPublicKey(pubKey)
	if perr != nil {
		return "", nil, false, fmt.Errorf("wrap public key: %w", perr)
	}
	base := ssh.MarshalAuthorizedKey(sshPub)
	pubLine := make([]byte, 0, len(base)+len(alias)+16)
	pubLine = append(pubLine, bytesTrimRight(base, "\n")...)
	pubLine = append(pubLine, []byte(" createos-cli "+alias+"\n")...)
	if werr := os.WriteFile(pub, pubLine, 0o600); werr != nil {
		return "", nil, false, fmt.Errorf("write %s: %w", pub, werr)
	}
	return priv, pubLine, true, nil
}

// removeDedicatedKey deletes the private + public key files for a
// sandbox alias. Returns whether anything was actually removed.
func removeDedicatedKey(alias string) bool {
	priv, pub, err := dedicatedKeyPath(alias)
	if err != nil {
		return false
	}
	removed := false
	if err := os.Remove(priv); err == nil {
		removed = true
	}
	if err := os.Remove(pub); err == nil {
		removed = true
	}
	return removed
}

func bytesTrimRight(b []byte, cutset string) []byte {
	for len(b) > 0 && strings.IndexByte(cutset, b[len(b)-1]) >= 0 {
		b = b[:len(b)-1]
	}
	return b
}

// hostLine builds the `Host` line — id-only, or dual alias when name is safe.
func hostLine(alias, name string) string {
	name = strings.TrimSpace(name)
	if name == "" || name == alias || !editorNameRE.MatchString(name) {
		return alias
	}
	return alias + " " + name
}

// renderSSHBlock builds the ~/.ssh/config stanza for the sandbox.
//
// HostKeyAlias pins each sandbox's known_hosts entry to its own id. Without it
// every tunnel-mode box is keyed on the shared `127.0.0.1:22`, so the second
// sandbox trips a CHANGED HOST KEY warning and clients that treat that as an
// attack (Orca's SSH relay, for one) refuse to connect. VPN mode keys on the
// overlay IP, which is also recycled between sandboxes, so it needs the same
// pin.
func renderSSHBlock(alias, mode, sandboxID, sbIP, gwHost string, gwPort int, user, identity, name string) (string, error) {
	// The directory the %n-keyed ControlPath below lands sockets in — ssh
	// creates the socket file itself but not its parent directory.
	if _, err := ensureMuxDir(); err != nil {
		return "", err
	}
	begin := fmt.Sprintf(sshConfigBlockBegin, alias)
	end := fmt.Sprintf(sshConfigBlockEnd, alias)
	host := hostLine(alias, name)
	switch mode {
	case "vpn":
		return fmt.Sprintf(`%s
Host %s
    HostName          %s
    HostKeyAlias      %s
    Port              22
    User              %s
    IdentityFile      %s
    StrictHostKeyChecking accept-new
    UserKnownHostsFile ~/.ssh/known_hosts_createos
    ControlPath       ~/.config/createos/mux/%%n
%s
`, begin, host, sbIP, sandboxID, user, identity, end), nil
	case "tunnel":
		// The inner `ssh -W` for the gateway needs its own
		// StrictHostKeyChecking + UserKnownHostsFile — it doesn't inherit
		// the outer Host block's options. Without these, the first
		// connect fails on unknown gateway host key.
		//
		// ControlPath must be pinned the same way HostKeyAlias is: every
		// tunnel-mode sandbox shares HostName 127.0.0.1 + User root, so a
		// ControlPath built from %h/%r/%p — including a common personal
		// `Host *` default — collides across every sandbox. ssh then reuses
		// another sandbox's stale multiplexed connection instead of opening
		// one to the box actually asked for. %n (the alias) is the one
		// token guaranteed unique per sandbox.
		return fmt.Sprintf(`%s
Host %s
    HostName          127.0.0.1
    HostKeyAlias      %s
    Port              22
    User              %s
    IdentityFile      %s
    ProxyCommand      ssh -o StrictHostKeyChecking=accept-new -o UserKnownHostsFile=~/.ssh/known_hosts_createos -W %%h:%%p %s@%s -p %d -i %s
    StrictHostKeyChecking accept-new
    UserKnownHostsFile ~/.ssh/known_hosts_createos
    ControlPath       ~/.config/createos/mux/%%n
%s
`, begin, host, sandboxID, user, identity, sandboxID, gwHost, gwPort, identity, end), nil
	default:
		return "", fmt.Errorf("unknown mode %q", mode)
	}
}

// writeSSHBlock atomically rewrites ~/.ssh/config, replacing any existing
// block for this alias. Preserves all other content.
func writeSSHBlock(alias, block string) error {
	path, err := sshConfigPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("mkdir ~/.ssh: %w", err)
	}
	existing, _ := os.ReadFile(path) //nolint:errcheck // #nosec G304 -- missing file is fine (fresh install); read failure falls through to write
	updated := replaceOrAppendBlock(string(existing), alias, block)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(updated), 0o600); err != nil { //nolint:gosec // path derives from user's own $HOME, alias regex-validated
		return fmt.Errorf("write ~/.ssh/config.tmp: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("rename ~/.ssh/config: %w", err)
	}
	return nil
}

// removeSSHBlock removes any existing block for the alias. Returns
// (removed, err) — removed=false means the block wasn't present.
func removeSSHBlock(alias string) (bool, error) {
	path, err := sshConfigPath()
	if err != nil {
		return false, err
	}
	existing, err := os.ReadFile(path) // #nosec G304 -- user's own ssh config
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	updated, removed := stripBlock(string(existing), alias)
	if !removed {
		return false, nil
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(updated), 0o600); err != nil { //nolint:gosec // path derives from user's own $HOME, alias regex-validated
		return false, err
	}
	if err := os.Rename(tmp, path); err != nil {
		return false, err
	}
	return true, nil
}

// replaceOrAppendBlock strips any existing block for alias, then appends
// the new one at the end. Blank-line trimming keeps the file tidy.
func replaceOrAppendBlock(existing, alias, block string) string {
	stripped, _ := stripBlock(existing, alias)
	stripped = strings.TrimRight(stripped, "\n")
	if stripped != "" {
		stripped += "\n\n"
	}
	return stripped + strings.TrimRight(block, "\n") + "\n"
}

// stripBlock removes the block for alias from existing. Returns the new
// content and whether anything was removed.
func stripBlock(existing, alias string) (string, bool) {
	begin := fmt.Sprintf(sshConfigBlockBegin, alias)
	end := fmt.Sprintf(sshConfigBlockEnd, alias)
	i := strings.Index(existing, begin)
	if i < 0 {
		return existing, false
	}
	j := strings.Index(existing[i:], end)
	if j < 0 {
		// Malformed — keep the file as-is rather than corrupt it further.
		return existing, false
	}
	// j is relative to i; advance past the end-marker line.
	cut := i + j + len(end)
	if cut < len(existing) && existing[cut] == '\n' {
		cut++
	}
	return existing[:i] + existing[cut:], true
}

func sshConfigPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve $HOME: %w", err)
	}
	return filepath.Join(home, ".ssh", "config"), nil
}

// -----------------------------------------------------------------------------
// helpers
// -----------------------------------------------------------------------------

// preflightVPN validates that (a) this machine is registered as a
// device, (b) the device and sandbox share at least one network, and
// (c) the VPN tunnel is up. Fails with a copy-pasteable next step
// instead of a stack trace.
func preflightVPN(c *cli.Context, client *api.SandboxClient, sandboxID string) error {
	st, _ := loadDeviceState() //nolint:errcheck // missing file = not registered
	if st == nil || st.DeviceID == "" {
		pterm.Warning.Println("this machine isn't set up for the VPN yet.")
		pterm.Println()
		pterm.Println("    createos sandbox devices register")
		pterm.Println()
		pterm.Info.Println("then re-run this command.")
		return fmt.Errorf("device not registered")
	}
	shared, err := sandboxSharesNetwork(c.Context, client, st.DeviceID, sandboxID)
	if err != nil {
		// Server-lost device (was deleted from account) → point user at
		// the fix instead of dumping the raw API error.
		if api.IsNotFound(err) {
			pterm.Warning.Println("your saved device registration is stale — re-register this machine:")
			pterm.Println()
			pterm.Println("    createos sandbox devices register")
			pterm.Println()
			return fmt.Errorf("device registration stale")
		}
		return fmt.Errorf("checking your device's networks: %w", err)
	}
	if !shared {
		pterm.Warning.Println("this sandbox and your device aren't in the same network yet.")
		pterm.Println()
		pterm.Println("  Add the sandbox to a network your device is in:")
		pterm.Println("    createos sandbox network attach <network> " + sandboxID)
		pterm.Println()
		pterm.Println("  Or add the device to a network the sandbox is in:")
		pterm.Println("    createos sandbox devices attach <network>")
		return fmt.Errorf("device and sandbox are not on the same network")
	}
	if !isVPNUp() {
		pterm.Warning.Println("VPN isn't running. Open a new terminal tab and start it:")
		pterm.Println()
		pterm.Println("    createos sandbox vpn up")
		pterm.Println()
		pterm.Info.Println("Leave it running (Ctrl-C stops it), then re-run this command.")
		return fmt.Errorf("vpn not up")
	}
	return nil
}

// sandboxSharesNetwork returns true when the device and sandbox share
// at least one private network. Uses ListDeviceNetworks + GetNetwork so
// no new server surface is required.
func sandboxSharesNetwork(ctx context.Context, client *api.SandboxClient, deviceID, sandboxID string) (bool, error) {
	nets, err := client.ListDeviceNetworks(ctx, deviceID)
	if err != nil {
		return false, err
	}
	for _, n := range nets {
		net, err := client.GetNetwork(ctx, n.NetworkID)
		if err != nil {
			continue
		}
		for _, m := range net.Members {
			if m.SandboxID == sandboxID {
				return true, nil
			}
		}
	}
	return false, nil
}

// isVPNUp cheaply probes whether the createos tunnel interface is
// active. macOS reads the wg-quick name file; Linux uses `wg show`.
func isVPNUp() bool {
	if _, err := os.Stat("/var/run/wireguard/cosvpn.name"); err == nil {
		return true
	}
	if _, err := exec.LookPath("wg"); err == nil {
		if err := exec.CommandContext(context.Background(), "wg", "show", "cosvpn").Run(); err == nil {
			return true
		}
	}
	return false
}

// gatewayAddr resolves the gateway host:port. CREATEOS_GATEWAY_HOST
// overrides for devs pointing at staging; default is the mizar EU
// gateway shipped to end users.
func gatewayAddr() (string, int) {
	host := strings.TrimSpace(os.Getenv("CREATEOS_GATEWAY_HOST"))
	if host == "" {
		host = "gateway.sb.createos.sh"
	}
	return host, 2222
}

// startGuestSshd runs the same prep script `shell --ssh` runs. Kept small
// so a future extraction into a shared internal/guest package is cheap.
func startGuestSshd(c *cli.Context, client *api.SandboxClient, id, user string) error {
	authPath := authorizedKeysPath(user)
	prepScript := fmt.Sprintf(`
set -e
if ! [ -x /usr/sbin/sshd ]; then
  echo "this image does not ship sshd — use a rootfs that does (e.g. devbox:1)" >&2
  exit 100
fi
mkdir -p %[1]s /run/sshd
chmod 700 %[1]s
chmod 600 %[1]s/authorized_keys
chown -R %[2]s:%[2]s %[1]s 2>/dev/null || true
if ! awk 'NR>1{print $2}' /proc/net/tcp /proc/net/tcp6 2>/dev/null | grep -qi ':0016$'; then
  /usr/sbin/sshd
fi
`, filepath.Dir(authPath), user)
	resp, err := client.ExecSandbox(c.Context, id, api.SandboxExecReq{
		Cmd:  "sh",
		Args: []string{"-c", prepScript},
	})
	if err != nil {
		return fmt.Errorf("prep sshd: %w", err)
	}
	if resp.Result.ExitCode == 100 {
		return fmt.Errorf("sandbox image doesn't have sshd — use a rootfs that does (e.g. devbox:1)")
	}
	if resp.Result.ExitCode != 0 {
		return fmt.Errorf("sshd prep failed: %s", strings.TrimSpace(resp.Result.Stderr))
	}
	return nil
}

// probeSSH runs `ssh -o BatchMode=yes -o ConnectTimeout=3 -G <alias>` to
// verify the config is parseable, then attempts a 1-second TCP probe by
// running `ssh -o BatchMode=yes -o ConnectTimeout=3 <alias> true`. Any
// non-nil error signals the caller to warn but not fail.
func probeSSH(ctx context.Context, alias string, wait time.Duration) error {
	deadline, cancel := context.WithTimeout(ctx, wait)
	defer cancel()
	last := fmt.Errorf("no attempt")
	for deadline.Err() == nil {
		// #nosec G204 -- alias is regex-validated (editorAliasRE ^sb-[0-9a-z]{26}$) before it lands here.
		cmd := exec.CommandContext(deadline, "ssh",
			"-o", "BatchMode=yes",
			"-o", "ConnectTimeout=3",
			"-o", "StrictHostKeyChecking=accept-new",
			alias, "true")
		if err := cmd.Run(); err == nil {
			return nil
		} else { //nolint:revive // clearer as-is; last needs setting on failure
			last = err
		}
		time.Sleep(time.Second)
	}
	return last
}

// launchEditor spawns the user's editor. Uses the friendly name when safe
// so the editor's status bar shows "nostalgic-cartwright" instead of a ULID.
func launchEditor(editor, alias, name string) error {
	display := alias
	if n := strings.TrimSpace(name); n != "" && n != alias && editorNameRE.MatchString(n) {
		display = n
	}
	var cmd *exec.Cmd
	switch editor {
	case "zed":
		cmd = exec.CommandContext(context.Background(), "zed", fmt.Sprintf("ssh://%s/root", display)) //nolint:gosec // G204 false positive; display validated
	case "cursor":
		cmd = exec.CommandContext(context.Background(), "cursor", "--folder-uri", "vscode-remote://ssh-remote+"+display+"/root") //nolint:gosec // G204 false positive; display validated
	case "code":
		cmd = exec.CommandContext(context.Background(), "code", "--remote", "ssh-remote+"+display, "/root") //nolint:gosec // G204 false positive; display validated
	default:
		return fmt.Errorf("unknown editor %q", editor)
	}
	// Detach — we don't want to block on the editor's process.
	cmd.Stdin = nil
	cmd.Stdout = nil
	cmd.Stderr = nil
	if runtime.GOOS != "windows" {
		// Don't inherit our controlling terminal.
		cmd.Env = os.Environ()
	}
	return cmd.Start()
}

// printFollowup shows the next-step hints when we didn't auto-launch.
func printFollowup(alias, name string) {
	display := alias
	if n := strings.TrimSpace(name); n != "" && n != alias && editorNameRE.MatchString(n) {
		display = n
	}
	pterm.Info.Println("connect anytime:")
	pterm.Info.Printfln("  ssh %s", display)
	pterm.Info.Printfln("  zed ssh://%s/root", display)
	pterm.Info.Printfln("  code --remote ssh-remote+%s /root", display)
	pterm.Info.Printfln("  cursor --folder-uri vscode-remote://ssh-remote+%s/root", display)
}

// sweepStaleBlocks prunes our config blocks whose sandbox is 404 or in
// a terminal state. Transient failures leave blocks alone.
func sweepStaleBlocks(ctx context.Context, client *api.SandboxClient, keep string) ([]string, error) {
	path, err := sshConfigPath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path) //nolint:gosec // #nosec G304 -- own ssh config
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	aliases := collectCreateosAliases(string(data))
	if len(aliases) == 0 {
		return nil, nil
	}

	// Check each alias in parallel — bounded pool of 8.
	type verdict struct {
		alias string
		gone  bool
	}
	sem := make(chan struct{}, 8)
	ch := make(chan verdict, len(aliases))
	for _, a := range aliases {
		if a == keep {
			continue
		}
		sem <- struct{}{}
		go func() {
			defer func() { <-sem }()
			gone := isSandboxGone(ctx, client, a)
			ch <- verdict{alias: a, gone: gone}
		}()
	}
	// Drain semaphore so we know all goroutines finished.
	for i := 0; i < cap(sem); i++ {
		sem <- struct{}{}
	}
	close(ch)

	pruned := make([]string, 0, len(aliases))
	for v := range ch {
		if !v.gone {
			continue
		}
		if _, rerr := removeSSHBlock(v.alias); rerr == nil {
			removeDedicatedKey(v.alias)
			pruned = append(pruned, v.alias)
		}
	}
	return pruned, nil
}

func collectCreateosAliases(cfg string) []string {
	out := make([]string, 0)
	const prefix = "# BEGIN createos "
	for _, line := range strings.Split(cfg, "\n") {
		if !strings.HasPrefix(line, prefix) {
			continue
		}
		alias := strings.TrimSpace(strings.TrimPrefix(line, prefix))
		if editorAliasRE.MatchString(alias) {
			out = append(out, alias)
		}
	}
	return out
}

// isSandboxGone returns true only on 404 or terminal status. Network
// errors return false — never nuke config on transient issues.
func isSandboxGone(ctx context.Context, client *api.SandboxClient, alias string) bool {
	sctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	sb, err := client.GetSandbox(sctx, alias)
	if err != nil {
		return api.IsNotFound(err)
	}
	switch sb.Status {
	case api.SandboxStatusDestroyed, api.SandboxStatusFailed:
		return true
	}
	return false
}

func plural(n int, singular, plural string) string {
	if n == 1 {
		return singular
	}
	return plural
}
