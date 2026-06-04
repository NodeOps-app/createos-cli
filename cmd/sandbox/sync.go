package sandbox

import (
	"context"
	"encoding/pem"
	"errors"
	"fmt"
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

	// 1. Pick / resolve the sandbox.
	ref := strings.TrimSpace(c.Args().First())
	var id string
	if ref == "" {
		if !tty {
			return fmt.Errorf("please provide a sandbox ID or name\n\n  Example:\n    createos sandbox sync my-box --local ~/work --remote /root/work")
		}
		pickedID, label, err := pickByStatus(c, client, "Sync with which sandbox?", "running")
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

	// 2. Local + remote paths. Prompt on TTY when missing; default the
	//    local side to the user's current directory (the common case —
	//    "sync this project I'm sitting in").
	localArg := strings.TrimSpace(c.String("local"))
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
	remote := strings.TrimSpace(c.String("remote"))
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

	local, err := validateLocalSyncPath(localArg, c.Bool("force"))
	if err != nil {
		return err
	}
	if err = validateRemoteSyncPath(remote); err != nil {
		return err
	}

	mutagenBin, err := ensureMutagen()
	if err != nil {
		return err
	}

	// 3. Resolve the SSH identity. Same auto-detect as `shell --ssh`.
	privPath, pubPath, err := resolveIdentity(c.String("identity"))
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

	user := strings.TrimSpace(c.String("user"))
	if user == "" {
		user = "root"
	}

	// 4. Install authorized_keys + start sshd. Mirror of the SSH-shell
	//    path so sync gets the same modes/sshd setup.
	authPath := authorizedKeysPath(user)
	if err = client.UploadFile(c.Context, id, authPath, bytesReader(pubBytes), int64(len(pubBytes))); err != nil {
		return fmt.Errorf("could not install your SSH key: %w", err)
	}
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
	if err = waitForTCP(ctx, bridge.localAddr, c.Duration("sshd-wait")); err != nil {
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
	_ = runMutagen(ctx, mutagenBin, wrapperEnv, "daemon", "stop") //nolint:errcheck

	pterm.Println(pterm.Gray(fmt.Sprintf("  syncing %s ⇄ %s:%s", local, refLabel(ref, id), remote)))
	createArgs := []string{
		"sync", "create",
		"--name=" + sessionName,
		"--ignore-vcs",
		local,
		remoteSpec,
	}
	if err := runMutagen(ctx, mutagenBin, wrapperEnv, createArgs...); err != nil {
		return fmt.Errorf("mutagen sync create failed: %w", err)
	}
	// Best-effort cleanup on exit. We can't always rely on context
	// cancellation propagating before we exit.
	defer func() {
		bg := context.Background()
		_ = runMutagen(bg, mutagenBin, wrapperEnv, "sync", "terminate", sessionName) //nolint:errcheck
	}()

	pterm.Success.Println("Sync running. Press Ctrl+C to stop.")

	// 7. Monitor the session in the foreground. `mutagen sync monitor`
	//    streams status lines until the session is terminated or the
	//    process exits.
	mon := exec.CommandContext(ctx, mutagenBin, "sync", "monitor", sessionName) // #nosec G204 -- mutagenBin is our managed binary; sessionName is internally generated
	mon.Env = wrapperEnv
	mon.Stdout = os.Stdout
	mon.Stderr = os.Stderr
	if err := mon.Run(); err != nil && ctx.Err() == nil {
		return fmt.Errorf("mutagen monitor exited: %w", err)
	}
	pterm.Println("Sync stopped.")
	return nil
}

// runMutagen runs `mutagen <args>` with our shadowed PATH env.
// stdout/stderr are forwarded so the user sees mutagen's progress.
func runMutagen(ctx context.Context, bin string, env []string, args ...string) error {
	cmd := exec.CommandContext(ctx, bin, args...) // #nosec G204 -- bin is our managed mutagen binary; args are internally constructed
	cmd.Env = env
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	return cmd.Run()
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
