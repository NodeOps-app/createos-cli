package sandbox

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/pterm/pterm"
	"github.com/urfave/cli/v2"

	"github.com/NodeOps-app/createos-cli/internal/api"
	"github.com/NodeOps-app/createos-cli/internal/dclock"
	"github.com/NodeOps-app/createos-cli/internal/sshkey"
)

// dcSyncSession represents one live Mutagen sync session held by a
// `dc up` foreground process. Close() tears down the bridge but
// LEAVES the mutagen session alive — mutagen pauses naturally when the
// SSH transport drops, and the next `dc up` resumes it.
//
// To FULLY destroy the session (terminate in mutagen, drop the
// lockfile sync entry), call terminateDCSync().
type dcSyncSession struct {
	name       string
	bridge     *tunnelBridge
	wrapperDir string
	wrapperEnv []string
	mutagenBin string
	tempKeyDir string // tempdir holding the decrypted key (if any)
}

func (s *dcSyncSession) Close() {
	if s == nil {
		return
	}
	if s.bridge != nil {
		s.bridge.close()
	}
	if s.wrapperDir != "" {
		_ = os.RemoveAll(s.wrapperDir) //nolint:errcheck
	}
	if s.tempKeyDir != "" {
		_ = os.RemoveAll(s.tempKeyDir) //nolint:errcheck
	}
}

// ensureDCSyncSession sets up the SSH key + sshd + tunnel bridge +
// Mutagen sync session for a project. Idempotent: re-runs detect the
// lockfile's sync entry and resume an existing mutagen session instead
// of recreating it.
//
// On success the bridge is held open inside the returned session — the
// caller must keep the process alive and Close() on shutdown.
func ensureDCSyncSession(
	c *cli.Context,
	client *api.SandboxClient,
	sandboxID, projectName, projectDir, remoteWorkdir string,
	lock *dclock.Lock,
) (*dcSyncSession, error) {
	// 1. SSH key — explicit, then ~/.ssh defaults, else generate ours.
	keyPath := strings.TrimSpace(c.String("identity"))
	if keyPath == "" && lock.Sync != nil {
		keyPath = lock.Sync.PrivKeyPath // re-use last session's key if available
	}
	pair, err := sshkey.ResolveOrGenerate(keyPath)
	if err != nil {
		return nil, fmt.Errorf("ssh key: %w", err)
	}
	if pair.Managed {
		pterm.Info.Println("Using managed SSH key ~/.createos/" + sshkey.ManagedKeyName + " (auto-generated)")
	}

	// 2. Decrypt key if passphrase-protected (mutagen can't prompt).
	unlocked, cleanup, err := unlockSSHKeyIfNeeded(pair.PrivPath)
	if err != nil {
		return nil, err
	}
	tempKeyDir := ""
	if unlocked != pair.PrivPath {
		tempKeyDir = filepath.Dir(unlocked)
	}
	defer func() {
		// We only call cleanup if we DON'T return the session
		// successfully; otherwise the session takes ownership.
		_ = cleanup
	}()

	pubBytes, err := os.ReadFile(pair.PubPath) // #nosec G304 -- user's own pubkey
	if err != nil {
		return nil, fmt.Errorf("read pub key %s: %w", pair.PubPath, err)
	}

	// 3. Install pubkey in sandbox if needed, start sshd, mkdir target.
	prepScript := fmt.Sprintf(`
set -e
if ! [ -x /usr/sbin/sshd ]; then
  echo "this image does not ship sshd — sync requires devbox:1" >&2
  exit 100
fi
mkdir -p /root/.ssh /run/sshd
chmod 700 /root/.ssh
mkdir -p %s
if ! awk 'NR>1{print $2}' /proc/net/tcp /proc/net/tcp6 2>/dev/null | grep -qi ':0016$'; then
  /usr/sbin/sshd
fi
`, shellQuote(remoteWorkdir))
	if akErr := ensureAuthorizedKey(c, client, sandboxID, "root", projectName, pubBytes, true /* assumeYes */); akErr != nil {
		return nil, fmt.Errorf("install ssh pubkey: %w", akErr)
	}
	if pre, execErr := client.ExecSandbox(c.Context, sandboxID, api.SandboxExecReq{
		Cmd:  "sh",
		Args: []string{"-c", prepScript},
	}); execErr != nil {
		return nil, fmt.Errorf("prep sshd: %w", execErr)
	} else if pre.Result.ExitCode == 100 {
		return nil, fmt.Errorf("sandbox image doesn't have sshd — use rootfs devbox:1 for sync")
	} else if pre.Result.ExitCode != 0 {
		return nil, fmt.Errorf("sshd prep failed: %s", strings.TrimSpace(pre.Result.Stderr))
	}

	// 4. Pin a deterministic local port per sandbox so the mutagen
	//    session URL stays valid across `dc up` re-runs.
	localPort := 0
	if lock.Sync != nil && lock.Sync.LocalSSHPort > 0 {
		localPort = lock.Sync.LocalSSHPort
	} else {
		localPort = derivedLocalPort(sandboxID)
	}
	bridge, err := startTunnelBridgeOn(c.Context, c, sandboxID, 22, localPort)
	if err != nil {
		// Port collision — fall back to ephemeral so we don't block
		// the user, and update the lockfile.
		pterm.Warning.Printfln("Pinned local port %d in use; falling back to a fresh port (mutagen session will be recreated)", localPort)
		bridge, err = startTunnelBridgeOn(c.Context, c, sandboxID, 22, 0)
		if err != nil {
			return nil, fmt.Errorf("open ssh tunnel: %w", err)
		}
		// Force-recreate: the saved session points at the old port.
		if lock.Sync != nil {
			lock.Sync = nil
		}
	}
	if wtErr := waitForTCP(c.Context, bridge.localAddr, 5*time.Second); wtErr != nil {
		bridge.close()
		return nil, fmt.Errorf("sshd not listening: %w", wtErr)
	}

	// 5. Set up the mutagen wrapper + locate the binary.
	mutagenBin, err := ensureMutagen()
	if err != nil {
		bridge.close()
		return nil, err
	}
	wrapperDir, wrapperEnv, err := makeSSHWrapper(unlocked)
	if err != nil {
		bridge.close()
		return nil, fmt.Errorf("ssh wrapper: %w", err)
	}
	// Force mutagen daemon restart so it picks up our PATH-shadowed ssh/scp.
	_ = runMutagen(c.Context, mutagenBin, wrapperEnv, "daemon", "stop") //nolint:errcheck

	// 6. Session name — deterministic per (project, sandbox) so re-runs
	//    can find and resume.
	sessionName := mutagenSessionName(projectName, sandboxID)
	port, perr := splitPort(bridge.localAddr)
	if perr != nil {
		bridge.close()
		return nil, fmt.Errorf("parse bridge addr %q: %w", bridge.localAddr, perr)
	}
	remoteSpec := fmt.Sprintf("root@127.0.0.1:%s:%s", port, remoteWorkdir)
	localPath, _ := filepath.Abs(projectDir) //nolint:errcheck

	// 7. Resume existing or create fresh.
	if sessionExists(c.Context, mutagenBin, wrapperEnv, sessionName) {
		pterm.Info.Println("Resuming existing sync session " + sessionName)
		if err := runMutagen(c.Context, mutagenBin, wrapperEnv, "sync", "resume", sessionName); err != nil {
			// Resume can fail if the session is half-broken; recreate.
			pterm.Warning.Println("resume failed; recreating session")
			_ = runMutagen(c.Context, mutagenBin, wrapperEnv, "sync", "terminate", sessionName) //nolint:errcheck
			if err := mutagenCreate(c.Context, mutagenBin, wrapperEnv, sessionName, localPath, remoteSpec); err != nil {
				bridge.close()
				_ = os.RemoveAll(wrapperDir) //nolint:errcheck
				return nil, err
			}
		}
	} else {
		pterm.Info.Printfln("Starting sync %s ⇄ sandbox:%s", localPath, remoteWorkdir)
		if err := mutagenCreate(c.Context, mutagenBin, wrapperEnv, sessionName, localPath, remoteSpec); err != nil {
			bridge.close()
			_ = os.RemoveAll(wrapperDir) //nolint:errcheck
			return nil, err
		}
	}

	// 8. Persist session info into the lockfile (caller saves the file).
	lock.Sync = &dclock.Sync{
		SessionName:  sessionName,
		LocalSSHPort: localPortFromAddr(bridge.localAddr),
		PrivKeyPath:  pair.PrivPath,
	}

	// 9. Wait for initial scan to settle so the next `docker compose up`
	//    sees the user's files.
	if err := waitForSyncReady(c.Context, mutagenBin, wrapperEnv, sessionName, 60*time.Second); err != nil {
		pterm.Warning.Println("initial sync didn't reach steady state in time: " + err.Error())
		// Non-fatal: compose may still work if compose file is present.
	} else {
		pterm.Success.Println("Sync ready")
	}

	return &dcSyncSession{
		name:       sessionName,
		bridge:     bridge,
		wrapperDir: wrapperDir,
		wrapperEnv: wrapperEnv,
		mutagenBin: mutagenBin,
		tempKeyDir: tempKeyDir,
	}, nil
}

// terminateDCSync stops + removes the sync session permanently. Used
// by `dc down` before destroying the sandbox.
func terminateDCSync(ctx context.Context, lock *dclock.Lock) error {
	if lock.Sync == nil || lock.Sync.SessionName == "" {
		return nil
	}
	bin, err := ensureMutagen()
	if err != nil {
		// Without the binary we can't talk to the daemon; best-effort.
		return nil //nolint:nilerr
	}
	// Use minimal env — the daemon should already know about the session.
	if err := runMutagen(ctx, bin, os.Environ(), "sync", "terminate", lock.Sync.SessionName); err != nil {
		return fmt.Errorf("mutagen terminate %s: %w", lock.Sync.SessionName, err)
	}
	return nil
}

// mutagenCreate wraps `mutagen sync create --name=<n> --ignore-vcs +
// our default ignores`.
func mutagenCreate(ctx context.Context, bin string, env []string, name, local, remoteSpec string) error {
	args := []string{
		"sync", "create",
		"--name=" + name,
		"--ignore-vcs",
		"--ignore=.createos/", // never sync our own lockfile
		"--ignore=node_modules/",
		"--ignore=__pycache__/",
		"--ignore=.venv/",
		"--ignore=target/",
		"--ignore=dist/",
		"--ignore=build/",
		// Force readable perms on the VM side. Without this mutagen
		// preserves source perms (often 0700 from `mkdir`) and most
		// in-container daemons run as a non-root uid that can't read
		// 0700-owned-by-root files. 0644 / 0755 is what `docker run -v`
		// users implicitly expect.
		"--default-file-mode-beta=0644",
		"--default-directory-mode-beta=0755",
		local,
		remoteSpec,
	}
	if err := runMutagen(ctx, bin, env, args...); err != nil {
		return fmt.Errorf("mutagen sync create: %w", err)
	}
	return nil
}

// sessionExists asks the mutagen daemon whether a named session is
// known. Robust to daemon-down (returns false), so a fresh boot path
// "just creates" rather than failing.
func sessionExists(ctx context.Context, bin string, env []string, name string) bool {
	cmd := exec.CommandContext(ctx, bin, "sync", "list", name) // #nosec G204 -- bin is managed, name is internal
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	if err != nil {
		return false
	}
	return strings.Contains(string(out), name)
}

// waitForSyncReady polls `mutagen sync list <name>` until the status
// shows "Watching for changes" (steady state) or the deadline fires.
func waitForSyncReady(ctx context.Context, bin string, env []string, name string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		cmd := exec.CommandContext(ctx, bin, "sync", "list", name) // #nosec G204 -- bin managed; name internal
		cmd.Env = env
		out, err := cmd.CombinedOutput()
		if err == nil {
			s := string(out)
			if strings.Contains(s, "Watching for changes") {
				return nil
			}
			if strings.Contains(s, "halted") || strings.Contains(s, "failed") {
				return fmt.Errorf("session halted: %s", s)
			}
		}
		time.Sleep(1 * time.Second)
	}
	return fmt.Errorf("timed out waiting for sync ready")
}

// derivedLocalPort returns a deterministic ephemeral port in
// [20000, 65000) keyed on the sandbox id, so re-runs against the same
// sandbox land on the same listener and the mutagen session URL stays
// valid.
func derivedLocalPort(sandboxID string) int {
	h := sha256.Sum256([]byte("createos-dc-sync:" + sandboxID))
	v := binary.BigEndian.Uint32(h[:4])
	const lo = 20000
	const hi = 65000
	return int(lo + v%(hi-lo))
}

// localPortFromAddr extracts the port from "host:port", returning 0 on
// parse failure (caller should treat as "not pinned, recreate next time").
func localPortFromAddr(addr string) int {
	port, err := splitPort(addr)
	if err != nil {
		return 0
	}
	var p int
	if _, err := fmt.Sscanf(port, "%d", &p); err != nil {
		return 0
	}
	return p
}

// splitPort returns just the port from "host:port". Used in two places
// where the host half is unwanted (lint flags unparam if we return it).
func splitPort(addr string) (string, error) {
	i := strings.LastIndex(addr, ":")
	if i < 0 {
		return "", errors.New("no port in addr")
	}
	return addr[i+1:], nil
}

// mutagenSessionName builds a deterministic session name for a
// (project, sandbox) pair. Stable across re-runs so resume works.
//
// Mutagen names are constrained to [-_a-z0-9]+ and capped around 50
// chars; we strip aggressively.
func mutagenSessionName(project, sandboxID string) string {
	safe := func(s string) string {
		var b strings.Builder
		for _, r := range strings.ToLower(s) {
			switch {
			case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-':
				b.WriteRune(r)
			case r == '_':
				b.WriteRune('-')
			}
		}
		return b.String()
	}
	name := "cdc-" + safe(project) + "-" + safe(sandboxID)
	if len(name) > 50 {
		name = name[:50]
	}
	return name
}
