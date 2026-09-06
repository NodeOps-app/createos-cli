package sandbox

import (
	"bytes"
	"context"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/pterm/pterm"
	"github.com/urfave/cli/v2"
	"golang.org/x/crypto/ssh"
	"golang.org/x/term"

	"github.com/NodeOps-app/createos-cli/internal/api"
	"github.com/NodeOps-app/createos-cli/internal/terminal"
)

// newSyncCommand wires up `createos sandbox sync` — a foreground
// bidirectional file sync between your laptop and a sandbox. Built on
// the same SSH path as `sandbox shell --ssh`: install the user's
// public key, start sshd, tunnel through the control plane to VM:22,
// then drive a Mutagen (https://mutagen.io) session over that.
//
// Lifecycle is tied to this process: Ctrl+C / EOF / sandbox crash
// all terminate the session. No daemon, no global state to clean up.
func newSyncCommand() *cli.Command {
	return &cli.Command{
		Name:      "sync",
		Usage:     "Two-way file sync between your laptop and a sandbox (foreground; Ctrl+C to stop)",
		ArgsUsage: "[<sandbox>]",
		Description: `Mirrors a local directory with one inside a running sandbox. Changes
on either side propagate to the other.

The first run downloads Mutagen — the sync engine we shell out to —
into the createos-cli cache, verified against a pinned sha256 hash.

Examples:
  createos sandbox sync my-box --local ~/work/project --remote /root/work
  createos sandbox sync my-box -i ~/.ssh/id_ed25519

  # Skip files you don't want synced (repeatable)
  createos sandbox sync my-box --exclude '*.log' --exclude node_modules

  # Push-only: laptop wins, never pull changes back
  createos sandbox sync my-box --mode one-way

  # Mirror: make the sandbox identical, deleting extra files there
  createos sandbox sync my-box --mode mirror

  # Run silently in the background of your terminal
  createos sandbox sync my-box --quiet

Safety: refuses to sync from $HOME directly, /, or known sensitive
paths (.ssh, .aws, etc.). Refuses to sync TO system dirs inside the
sandbox (/etc, /usr, /bin …). Pass --force to bypass the local check
(the remote check stays enforced).`,
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:  "local",
				Usage: "Local directory to sync (asks interactively if omitted)",
			},
			&cli.StringFlag{
				Name:  "remote",
				Usage: "Remote directory inside the sandbox (asks interactively if omitted; absolute path)",
			},
			&cli.StringFlag{
				Name:    "identity",
				Aliases: []string{"i"},
				Usage:   "Path to your SSH private key (default: ~/.ssh/id_ed25519, then id_rsa, then id_ecdsa)",
			},
			&cli.StringFlag{
				Name:    "user",
				Aliases: []string{"u"},
				Value:   "root",
				Usage:   "Username inside the sandbox",
			},
			&cli.DurationFlag{
				Name:  "sshd-wait",
				Value: 5 * time.Second,
				Usage: "How long to wait for sshd to bind :22 inside the sandbox",
			},
			&cli.BoolFlag{
				Name:  "force",
				Usage: "Bypass the local sensitive-path check (still requires non-/ paths)",
			},
			&cli.BoolFlag{
				Name:    "yes",
				Aliases: []string{"y"},
				Usage:   "Install your SSH key into the sandbox without asking (required in non-interactive mode when your key isn't already there)",
			},
			&cli.StringSliceFlag{
				Name:  "exclude",
				Usage: "Glob pattern to skip; repeatable (e.g. --exclude '*.log' --exclude node_modules)",
			},
			&cli.StringFlag{
				Name:  "mode",
				Value: "two-way",
				Usage: "Sync direction: two-way | one-way (laptop wins, keeps extra files on the sandbox) | mirror (one-way and deletes extra files on the sandbox)",
			},
			&cli.BoolFlag{
				Name:    "quiet",
				Aliases: []string{"q"},
				Usage:   "Don't print status; run silently until Ctrl+C",
			},
			&cli.BoolFlag{
				Name:  "no-ignore-vcs",
				Usage: "Sync VCS directories too (.git, .hg …); by default they're skipped",
			},
		},
		Action: runSync,
	}
}

func runSync(c *cli.Context) error {
	client, ok := c.App.Metadata[api.SandboxClientKey].(*api.SandboxClient)
	if !ok {
		return fmt.Errorf("you're not signed in — run 'createos login' to get started")
	}
	tty := terminal.IsInteractive()

	// urfave/cli v2 stops parsing flags at the first positional, so
	// `sync my-box --mode mirror` would otherwise drop every flag after
	// the sandbox argument. parseSyncArgs recovers them regardless of
	// position (same workaround as parseDiskCreateArgs / splitForceFlag).
	opts := parseSyncArgs(c)

	// 1. Pick / resolve the sandbox.
	ref := opts.ref
	var id string
	if ref == "" {
		if !tty {
			return fmt.Errorf("please provide a sandbox ID or name\n\n  Example:\n    createos sandbox sync my-box --local ~/work --remote /root/work")
		}
		pickedID, label, err := pickByStatus(c, client, "Sync with which sandbox?", api.SandboxStatusRunning)
		if err != nil {
			return err
		}
		if pickedID == "" {
			fmt.Println("Cancelled. Nothing synced.")
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
	if err := ensureSandboxRunningFor(c, client, ref, id, "sync"); err != nil {
		return err
	}

	// 2. Local + remote paths. Prompt on TTY when missing; default the
	//    local side to the user's current directory (the common case —
	//    "sync this project I'm sitting in").
	localArg := opts.local
	if localArg == "" {
		if !tty {
			return errors.New("--local is required (no terminal for interactive prompt)")
		}
		cwd, _ := os.Getwd() //nolint:errcheck
		v, err := pterm.DefaultInteractiveTextInput.
			WithDefaultText("Local directory to sync (enter for current directory)").
			WithDefaultValue(cwd).
			Show()
		if err != nil {
			return fmt.Errorf("could not read local path: %w", err)
		}
		localArg = strings.TrimSpace(v)
		if localArg == "" {
			localArg = cwd
		}
	}
	remote := opts.remote
	if remote == "" {
		if !tty {
			return errors.New("--remote is required (no terminal for interactive prompt)")
		}
		v, err := pterm.DefaultInteractiveTextInput.
			WithDefaultText("Remote directory inside the sandbox (e.g. /root/work)").
			WithDefaultValue("/root/sync").
			Show()
		if err != nil {
			return fmt.Errorf("could not read remote path: %w", err)
		}
		remote = strings.TrimSpace(v)
	}

	local, err := validateLocalSyncPath(localArg, opts.force)
	if err != nil {
		return err
	}
	if err = validateRemoteSyncPath(remote); err != nil {
		return err
	}

	// Resolve --mode up front so a typo fails before we touch the
	// sandbox or download Mutagen.
	syncMode, err := syncModeToMutagen(opts.mode)
	if err != nil {
		return err
	}

	mutagenBin, err := ensureMutagen()
	if err != nil {
		return err
	}

	// 3. Resolve the SSH identity. Same auto-detect as `shell --ssh`.
	privPath, pubPath, err := resolveIdentity(opts.identity)
	if err != nil {
		return err
	}
	pubBytes, err := os.ReadFile(pubPath) // #nosec G304 -- pubPath is the user's own SSH public key, chosen via --identity
	if err != nil {
		return fmt.Errorf("could not read public key %s: %w", pubPath, err)
	}

	// 3b. Mutagen forks ssh from its background daemon and proxies any
	//     passphrase prompt through an RPC channel that doesn't survive
	//     this command — so a passphrase-protected key fails the
	//     handshake silently. Decrypt up front and point the ssh
	//     wrapper at the cleartext copy in a tempfile (mode 0600,
	//     deleted on exit).
	unlocked, cleanup, err := unlockSSHKeyIfNeeded(privPath)
	if err != nil {
		return err
	}
	defer cleanup()
	privPath = unlocked

	user := opts.user
	if user == "" {
		user = "root"
	}

	// 4. Install authorized_keys (with consent) + start sshd. Mirror of
	//    the SSH-shell path so sync gets the same modes/sshd setup.
	if err = ensureAuthorizedKey(c, client, id, user, ref, pubBytes, opts.assumeYes); err != nil {
		return err
	}
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
# Also create the remote target so mutagen has a place to land.
mkdir -p %[3]s
if ! awk 'NR>1{print $2}' /proc/net/tcp /proc/net/tcp6 2>/dev/null | grep -qi ':0016$'; then
  /usr/sbin/sshd
fi
`, filepath.Dir(authPath), user, shellQuote(remote))
	if pre, execErr := client.ExecSandbox(c.Context, id, api.SandboxExecReq{
		Cmd:  "sh",
		Args: []string{"-c", prepScript},
	}); execErr != nil {
		return fmt.Errorf("could not prepare sshd: %w", execErr)
	} else if pre.Result.ExitCode == 100 {
		return fmt.Errorf("the sandbox image doesn't have sshd installed — try a rootfs that does (e.g. devbox:1)")
	} else if pre.Result.ExitCode != 0 {
		return fmt.Errorf("sshd prep failed: %s", strings.TrimSpace(pre.Result.Stderr))
	}

	// 5. Tunnel through control to VM:22 — same bridge as `shell --ssh`.
	ctx, cancel := context.WithCancel(c.Context)
	defer cancel()
	bridge, err := startTunnelBridge(ctx, c, id, 22)
	if err != nil {
		return fmt.Errorf("could not open tunnel to the sandbox: %w", err)
	}
	defer bridge.close()
	if err = waitForTCP(ctx, bridge.localAddr, opts.sshdWait); err != nil {
		return fmt.Errorf("sshd did not start in time: %w", err)
	}
	_, port, _ := net.SplitHostPort(bridge.localAddr) //nolint:errcheck

	// 6. Create the mutagen session.
	//    Mutagen's URL parser dislikes `ssh://user@host:port/path` —
	//    `user@host:port:path` (triple-colon) works reliably.
	sessionName := fmt.Sprintf("createos-%s-%d", strings.ReplaceAll(id, "_", "-"), time.Now().Unix())
	remoteSpec := fmt.Sprintf("%s@127.0.0.1:%s:%s", user, port, remote)

	wrapperDir, wrapperEnv, err := makeSSHWrapper(privPath)
	if err != nil {
		return fmt.Errorf("could not set up ssh wrapper: %w", err)
	}
	defer func() { _ = os.RemoveAll(wrapperDir) }() //nolint:errcheck

	// Mutagen runs ssh from its long-lived daemon, not from this
	// process. Stop the daemon so the next `create` auto-starts it
	// under our env, picking up the wrapper PATH.
	_ = runMutagen(ctx, mutagenBin, wrapperEnv, io.Discard, io.Discard, "daemon", "stop") //nolint:errcheck

	quiet := opts.quiet
	if !quiet {
		pterm.Println(pterm.Gray(fmt.Sprintf("  syncing %s ⇄ %s:%s", local, refLabel(ref, id), remote)))
	}
	createArgs := mutagenCreateArgs(sessionName, syncMode, local, remoteSpec, !opts.noIgnoreVCS, opts.exclude)
	// On --quiet, capture create output and surface it only if the
	// command fails, so a real error isn't swallowed by the quiet flag.
	var createOut io.Writer = os.Stderr
	var createBuf *bytes.Buffer
	if quiet {
		createBuf = &bytes.Buffer{}
		createOut = createBuf
	}
	if err := runMutagen(ctx, mutagenBin, wrapperEnv, createOut, createOut, createArgs...); err != nil {
		detail := ""
		if createBuf != nil {
			if s := strings.TrimSpace(createBuf.String()); s != "" {
				detail = "\n" + s
			}
		}
		return fmt.Errorf("mutagen sync create failed: %w%s", err, detail)
	}
	// Best-effort cleanup on exit. We can't always rely on context
	// cancellation propagating before we exit.
	defer func() {
		bg := context.Background()
		_ = runMutagen(bg, mutagenBin, wrapperEnv, io.Discard, io.Discard, "sync", "terminate", sessionName) //nolint:errcheck
	}()

	if !quiet {
		pterm.Success.Println("Sync running. Press Ctrl+C to stop.")
	}

	// 7. Monitor the session in the foreground. `mutagen sync monitor`
	//    streams status lines until the session is terminated or the
	//    process exits — it blocks, which keeps the sync alive. When
	//    --quiet is set we still run it (for the blocking lifecycle) but
	//    drop its status output; errors stay on stderr.
	mon := exec.CommandContext(ctx, mutagenBin, "sync", "monitor", sessionName) // #nosec G204 -- mutagenBin is our managed binary; sessionName is internally generated
	mon.Env = wrapperEnv
	if quiet {
		mon.Stdout = io.Discard
	} else {
		mon.Stdout = os.Stdout
	}
	mon.Stderr = os.Stderr
	if err := mon.Run(); err != nil && ctx.Err() == nil {
		return fmt.Errorf("mutagen monitor exited: %w", err)
	}
	if !quiet {
		pterm.Println("Sync stopped.")
	}
	return nil
}

// syncModeToMutagen maps our friendly --mode values onto Mutagen's
// --sync-mode. We surface three of Mutagen's modes under plain names so
// users don't have to learn Mutagen's vocabulary:
//
//	two-way → two-way-safe    (default; conflicting edits pause, never clobber)
//	one-way → one-way-safe    (laptop is the source; extra files on the sandbox are kept)
//	mirror  → one-way-replica (laptop is the source; the sandbox is made identical, extras deleted)
func syncModeToMutagen(mode string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "", "two-way":
		return "two-way-safe", nil
	case "one-way":
		return "one-way-safe", nil
	case "mirror":
		return "one-way-replica", nil
	default:
		return "", fmt.Errorf("unknown --mode %q\n\n  Choose one of:\n    two-way  (default)\n    one-way\n    mirror", mode)
	}
}

// runMutagen runs `mutagen <args>` with our shadowed PATH env, sending
// stdout/stderr to the supplied writers. Callers decide the output
// policy (forward to the terminal, discard, or capture) rather than this
// helper hardwiring it — e.g. --quiet captures create output and only
// surfaces it on failure.
func runMutagen(ctx context.Context, bin string, env []string, stdout, stderr io.Writer, args ...string) error {
	cmd := exec.CommandContext(ctx, bin, args...) // #nosec G204 -- bin is our managed mutagen binary; args are internally constructed
	cmd.Env = env
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	return cmd.Run()
}

// mutagenCreateArgs assembles the argument list for `mutagen sync
// create`. Kept pure (no cli.Context, no I/O) so the mode/ignore/exclude
// mapping — especially the destructive `mirror` → one-way-replica path —
// is unit-testable. Source and target must be the final two positionals.
func mutagenCreateArgs(sessionName, syncMode, local, remoteSpec string, ignoreVCS bool, exclude []string) []string {
	args := []string{
		"sync", "create",
		"--name=" + sessionName,
		"--sync-mode=" + syncMode,
	}
	if ignoreVCS {
		args = append(args, "--ignore-vcs")
	}
	for _, pat := range exclude {
		if p := strings.TrimSpace(pat); p != "" {
			args = append(args, "--ignore="+p)
		}
	}
	return append(args, local, remoteSpec)
}

// syncOptions holds every resolved input to `sandbox sync`. It exists so
// flags work whether they appear before or after the sandbox argument
// (urfave/cli v2 stops flag parsing at the first positional).
type syncOptions struct {
	ref         string
	local       string
	remote      string
	identity    string
	user        string
	mode        string
	sshdWait    time.Duration
	exclude     []string
	force       bool
	assumeYes   bool
	quiet       bool
	noIgnoreVCS bool
}

// parseSyncArgs merges the flags urfave already parsed (those placed
// before the sandbox argument) with a manual scan of the positional tail
// (flags placed after it). The first bare token is the sandbox ref; for
// scalars the last value wins, and --exclude accumulates. Mirrors the
// parseDiskCreateArgs / splitForceFlag workaround used elsewhere here.
func parseSyncArgs(c *cli.Context) syncOptions {
	opts := syncOptions{
		local:       strings.TrimSpace(c.String("local")),
		remote:      strings.TrimSpace(c.String("remote")),
		identity:    strings.TrimSpace(c.String("identity")),
		user:        strings.TrimSpace(c.String("user")),
		mode:        c.String("mode"),
		sshdWait:    c.Duration("sshd-wait"),
		exclude:     append([]string{}, c.StringSlice("exclude")...),
		force:       c.Bool("force"),
		assumeYes:   c.Bool("yes"),
		quiet:       c.Bool("quiet"),
		noIgnoreVCS: c.Bool("no-ignore-vcs"),
	}

	// Map short aliases to their canonical flag names.
	canon := func(k string) string {
		switch k {
		case "i":
			return "identity"
		case "u":
			return "user"
		case "y":
			return "yes"
		case "q":
			return "quiet"
		}
		return k
	}

	args := c.Args().Slice()
	for i := 0; i < len(args); i++ {
		a := strings.TrimSpace(args[i])
		if a == "" {
			continue
		}
		if !strings.HasPrefix(a, "-") {
			if opts.ref == "" {
				opts.ref = a
			}
			continue
		}
		raw := strings.TrimLeft(a, "-")
		key, inline, hasInline := raw, "", false
		if eq := strings.IndexByte(raw, '='); eq >= 0 {
			key, inline, hasInline = raw[:eq], raw[eq+1:], true
		}
		key = canon(key)

		switch key {
		case "force":
			opts.force = true
		case "yes":
			opts.assumeYes = true
		case "quiet":
			opts.quiet = true
		case "no-ignore-vcs":
			opts.noIgnoreVCS = true
		case "local", "remote", "identity", "user", "mode", "sshd-wait", "exclude":
			val := inline
			if !hasInline && i+1 < len(args) {
				val = args[i+1]
				i++
			}
			val = strings.TrimSpace(val)
			switch key {
			case "local":
				opts.local = val
			case "remote":
				opts.remote = val
			case "identity":
				opts.identity = val
			case "user":
				opts.user = val
			case "mode":
				opts.mode = val
			case "sshd-wait":
				if d, derr := time.ParseDuration(val); derr == nil {
					opts.sshdWait = d
				}
			case "exclude":
				if val != "" {
					opts.exclude = append(opts.exclude, val)
				}
			}
		default:
			// Unknown flag after the positional. Ignore it rather than
			// guess whether it consumes the following token as a value.
		}
	}
	return opts
}

// makeSSHWrapper creates a tempdir with `ssh` AND `scp` shims that
// forward to the real binaries while injecting `-i <key>` and the
// right host-key flags. Returns (dir, env) where env's PATH has dir
// prepended. Caller is responsible for `os.RemoveAll(dir)`.
//
// Why this exists: mutagen 0.18 has no per-session SSH flag passthrough.
// It invokes whichever `ssh` is on PATH for the control channel AND
// forks `scp` directly to push its agent binary. Both need the same
// key + host-key policy, so we shadow both.
func makeSSHWrapper(privPath string) (string, []string, error) {
	realSSH, err := exec.LookPath("ssh")
	if err != nil {
		return "", nil, fmt.Errorf("system ssh not found: %w", err)
	}
	realSCP, err := exec.LookPath("scp")
	if err != nil {
		return "", nil, fmt.Errorf("system scp not found: %w", err)
	}
	dir, err := os.MkdirTemp("", "createos-ssh-wrapper-*")
	if err != nil {
		return "", nil, err
	}
	knownHosts := filepath.Join(dir, "known_hosts")
	commonOpts := fmt.Sprintf(
		"-i %s -o IdentitiesOnly=yes -o IdentityAgent=none "+
			"-o StrictHostKeyChecking=accept-new "+
			"-o UserKnownHostsFile=%s -o LogLevel=ERROR",
		shellQuote(privPath), shellQuote(knownHosts))

	// #nosec G306 -- these are wrapper scripts that must be executable
	if err := os.WriteFile(
		filepath.Join(dir, "ssh"),
		[]byte(fmt.Sprintf("#!/bin/sh\nexec %s %s \"$@\"\n", realSSH, commonOpts)),
		0o755,
	); err != nil {
		_ = os.RemoveAll(dir) //nolint:errcheck
		return "", nil, err
	}
	// #nosec G306 -- these are wrapper scripts that must be executable
	if err := os.WriteFile(
		filepath.Join(dir, "scp"),
		[]byte(fmt.Sprintf("#!/bin/sh\nexec %s %s \"$@\"\n", realSCP, commonOpts)),
		0o755,
	); err != nil {
		_ = os.RemoveAll(dir) //nolint:errcheck
		return "", nil, err
	}

	// Prepend the wrapper dir to PATH for the spawned mutagen process.
	env := os.Environ()
	for i, e := range env {
		if strings.HasPrefix(e, "PATH=") {
			env[i] = "PATH=" + dir + string(os.PathListSeparator) + strings.TrimPrefix(e, "PATH=")
			return dir, env, nil
		}
	}
	env = append(env, "PATH="+dir)
	return dir, env, nil
}

// shellQuote single-quotes a string for safe inclusion in /bin/sh.
// Embedded single quotes are escaped via the standard close-escape-open
// dance: 'foo'\”bar' decodes to foo'bar.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// ── authorized_keys consent ───────────────────────────────────────────

// ensureAuthorizedKey guarantees the local public key in pubBytes is
// present in the sandbox user's authorized_keys before sync/shell rely on
// SSH. It is idempotent and non-destructive:
//
//   - if a key matching ours is already installed, it returns immediately,
//     touching nothing (no upload, no prompt);
//   - if our key is NOT there, it asks for consent — the only path that
//     modifies the sandbox — then APPENDS our key, preserving any keys
//     already present.
//
// Consent rules mirror `sandbox rm`:
//   - interactive TTY  → y/N confirm
//   - non-interactive  → requires assumeYes, else a clear error
//   - assumeYes (--yes/-y) skips the prompt everywhere
func ensureAuthorizedKey(c *cli.Context, client *api.SandboxClient, id, user, ref string, pubBytes []byte, assumeYes bool) error {
	authPath := authorizedKeysPath(user)

	wantKey, _, _, _, perr := ssh.ParseAuthorizedKey(pubBytes)
	if perr != nil {
		return fmt.Errorf("your public key doesn't look like a valid SSH key: %w", perr)
	}
	want := canonicalAuthKey(wantKey)

	existing := readSandboxAuthorizedKeys(c, client, id, authPath)
	for _, line := range existing {
		if pk, _, _, _, e := ssh.ParseAuthorizedKey([]byte(line)); e == nil && canonicalAuthKey(pk) == want {
			// Already trusted — nothing to do. No overwrite, no prompt.
			return nil
		}
	}

	// Our key isn't there → installing it modifies the sandbox. Gate it.
	if !assumeYes {
		if !terminal.IsInteractive() {
			return fmt.Errorf("your SSH key isn't installed in %s yet\n\n  Installing it changes the sandbox's authorized_keys. Re-run with --yes to allow it:\n    createos sandbox %s --yes %s", refLabel(ref, id), c.Command.Name, ref)
		}
		prompt := fmt.Sprintf("Install your SSH key (%s) into %s?", ssh.FingerprintSHA256(wantKey), refLabel(ref, id))
		if n := len(existing); n > 0 {
			prompt += fmt.Sprintf(" It already has %d other key(s); yours is added alongside them.", n)
		}
		ok, cerr := pterm.DefaultInteractiveConfirm.
			WithDefaultText(prompt).
			WithDefaultValue(true).
			Show()
		if cerr != nil {
			return fmt.Errorf("could not read confirmation: %w", cerr)
		}
		if !ok {
			return errors.New("cancelled — your SSH key was not installed, so there's no way to connect")
		}
	}

	// Append our key, preserving existing entries (drop blank lines).
	merged := make([]string, 0, len(existing)+1)
	for _, l := range existing {
		if t := strings.TrimSpace(l); t != "" {
			merged = append(merged, t)
		}
	}
	merged = append(merged, strings.TrimSpace(string(pubBytes)))
	content := strings.Join(merged, "\n") + "\n"
	if err := client.UploadFile(c.Context, id, authPath, bytesReader([]byte(content)), int64(len(content))); err != nil {
		return fmt.Errorf("could not install your SSH key: %w", err)
	}
	return nil
}

// canonicalAuthKey reduces a public key to its "<type> <base64>" form,
// dropping the trailing newline and any comment, so two keys compare equal
// regardless of the comment they were uploaded with.
func canonicalAuthKey(pk ssh.PublicKey) string {
	return strings.TrimSpace(string(ssh.MarshalAuthorizedKey(pk)))
}

// readSandboxAuthorizedKeys returns the current authorized_keys lines for
// the sandbox user, or nil when the file is missing/unreadable (a fresh
// box). Errors are swallowed: a missing file is the common, expected case
// and simply means "no keys yet".
func readSandboxAuthorizedKeys(c *cli.Context, client *api.SandboxClient, id, authPath string) []string {
	resp, err := client.ExecSandbox(c.Context, id, api.SandboxExecReq{
		Cmd:  "sh",
		Args: []string{"-c", fmt.Sprintf("cat %s 2>/dev/null || true", shellQuote(authPath))},
	})
	if err != nil || resp.Result.ExitCode != 0 {
		return nil
	}
	var out []string
	for _, l := range strings.Split(resp.Result.Stdout, "\n") {
		if strings.TrimSpace(l) != "" {
			out = append(out, l)
		}
	}
	return out
}

// keyConsentGiven reports whether the user pre-authorized installing their
// SSH key, via --yes/-y. It also scans positional args because urfave/cli
// v2 stops parsing flags at the first positional, so `sync my-box --yes`
// would otherwise drop the flag (same workaround as `sandbox rm`).
func keyConsentGiven(c *cli.Context) bool {
	if c.Bool("yes") {
		return true
	}
	for _, a := range c.Args().Slice() {
		switch strings.TrimSpace(a) {
		case "-y", "--yes", "-yes":
			return true
		}
	}
	return false
}

// ── path validators ───────────────────────────────────────────────

// sensitiveLocalDirs is the set of directory NAMES we refuse to sync
// from. Anything whose first path component AFTER $HOME is in this set
// (or anything BENEATH it) is rejected unless --force is set.
var sensitiveLocalDirs = map[string]struct{}{
	".ssh": {}, ".gnupg": {}, ".aws": {}, ".config": {}, ".docker": {},
	".kube": {}, ".gcloud": {}, ".azure": {},
}

// validateLocalSyncPath verifies that `p` is a real directory under
// $HOME or /tmp, isn't $HOME itself, and isn't (or is under) a known
// sensitive directory. --force bypasses the sensitive check.
func validateLocalSyncPath(p string, force bool) (string, error) {
	if p == "" {
		return "", errors.New("local path is required")
	}
	// Expand ~ to $HOME.
	if strings.HasPrefix(p, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve $HOME: %w", err)
		}
		p = filepath.Join(home, strings.TrimPrefix(p, "~/"))
	} else if p == "~" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve $HOME: %w", err)
		}
		p = home
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", fmt.Errorf("resolve %q: %w", p, err)
	}
	info, err := os.Stat(abs)
	if err != nil {
		return "", fmt.Errorf("could not stat %s: %w", abs, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("%s is not a directory", abs)
	}
	if abs == "/" {
		return "", fmt.Errorf("refusing to sync from %q — pick a specific subdirectory", abs)
	}
	home, _ := os.UserHomeDir() //nolint:errcheck
	if abs == home {
		return "", fmt.Errorf("refusing to sync from $HOME itself — pick a subdirectory like ~/work")
	}
	allowed := false
	if home != "" && strings.HasPrefix(abs+"/", home+"/") {
		allowed = true
		if !force {
			// First component under $HOME — refuse known-sensitive ones.
			rel := strings.TrimPrefix(strings.TrimPrefix(abs, home), "/")
			first := strings.SplitN(rel, "/", 2)[0]
			if _, bad := sensitiveLocalDirs[first]; bad {
				return "", fmt.Errorf("refusing to sync from %s (sensitive directory) — pass --force if you really mean it", abs)
			}
		}
	}
	if strings.HasPrefix(abs+"/", "/tmp/") {
		allowed = true
	}
	if !allowed {
		return "", fmt.Errorf("local path must be under $HOME or /tmp (got %s)", abs)
	}
	return abs, nil
}

// reservedRemoteDirs are remote first-path-components we refuse to
// sync TO. System dirs that mutagen would happily overwrite.
var reservedRemoteDirs = map[string]struct{}{
	"": {}, "/": {}, "etc": {}, "usr": {}, "bin": {}, "sbin": {},
	"lib": {}, "lib64": {}, "boot": {}, "proc": {}, "sys": {}, "dev": {},
	"run": {},
}

// validateRemoteSyncPath checks the sandbox-side target. Must be
// absolute and not a system directory.
func validateRemoteSyncPath(p string) error {
	p = strings.TrimSpace(p)
	if p == "" {
		return errors.New("remote path is required")
	}
	if !strings.HasPrefix(p, "/") {
		return fmt.Errorf("remote path must be absolute (got %q)", p)
	}
	if p == "/" {
		return fmt.Errorf("refusing to sync to %q", p)
	}
	clean := strings.TrimPrefix(filepath.Clean(p), "/")
	first := strings.SplitN(clean, "/", 2)[0]
	if _, bad := reservedRemoteDirs[first]; bad {
		return fmt.Errorf("refusing to sync to %s (system path)", p)
	}
	return nil
}

// unlockSSHKeyIfNeeded reads the private key at path. If it's a
// passphrase-protected OpenSSH key, prompts the user once for the
// passphrase, decrypts it, and writes the cleartext form to a fresh
// 0600 file in a tempdir. Returns the path to use for ssh and a
// cleanup function that removes the tempdir. If the key is already
// unencrypted, returns the original path and a no-op cleanup.
//
// The decrypted file is only as safe as the user's tmp dir — but it
// never touches a shared location and is removed on `defer cleanup()`
// before this command returns.
func unlockSSHKeyIfNeeded(path string) (string, func(), error) {
	noop := func() {}
	raw, err := os.ReadFile(path) // #nosec G304 -- path is the user's own SSH private key, chosen via --identity
	if err != nil {
		return "", noop, fmt.Errorf("could not read SSH key %s: %w", path, err)
	}
	if _, perr := ssh.ParseRawPrivateKey(raw); perr == nil {
		// Already unencrypted, use as-is.
		return path, noop, nil
	}
	if !terminal.IsInteractive() {
		return "", noop, fmt.Errorf("the SSH key %s is passphrase-protected — pass --identity <unencrypted-key> or run from a terminal that can prompt for the passphrase", path)
	}
	fmt.Printf("Enter passphrase for %s: ", path)
	pw, perr := term.ReadPassword(int(os.Stdin.Fd())) // #nosec G115 -- a file descriptor always fits in an int
	fmt.Println()
	if perr != nil {
		return "", noop, fmt.Errorf("could not read passphrase: %w", perr)
	}
	key, perr := ssh.ParseRawPrivateKeyWithPassphrase(raw, pw)
	if perr != nil {
		return "", noop, fmt.Errorf("could not decrypt SSH key %s — wrong passphrase?", path)
	}
	block, perr := ssh.MarshalPrivateKey(key, "")
	if perr != nil {
		return "", noop, fmt.Errorf("could not re-encode SSH key: %w", perr)
	}

	dir, derr := os.MkdirTemp("", "createos-key-*")
	if derr != nil {
		return "", noop, derr
	}
	out := filepath.Join(dir, "id_unlocked")
	if werr := os.WriteFile(out, pem.EncodeToMemory(block), 0o600); werr != nil { // #nosec G703 -- out is under a freshly created MkdirTemp dir, not user-controlled
		_ = os.RemoveAll(dir) //nolint:errcheck
		return "", noop, fmt.Errorf("could not write unlocked key: %w", werr)
	}
	return out, func() { _ = os.RemoveAll(dir) }, nil //nolint:errcheck
}
