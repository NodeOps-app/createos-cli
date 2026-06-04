package sandbox

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/pterm/pterm"
	"github.com/urfave/cli/v2"
	"golang.org/x/term"

	"github.com/NodeOps-app/createos-cli/internal/api"
	"github.com/NodeOps-app/createos-cli/internal/config"
	"github.com/NodeOps-app/createos-cli/internal/terminal"
)

func newShellCommand() *cli.Command {
	return &cli.Command{
		Name:      "shell",
		Aliases:   []string{"sh"},
		Usage:     "Open an interactive shell inside a sandbox",
		ArgsUsage: "[<sandbox>]",
		Description: `Open a real terminal session inside a sandbox.
Works with tools that need a TTY — vim, htop, bash prompts.

By default this opens a PTY directly through the control plane — no
SSH keys, no sshd setup. Your existing API token is the only auth.

Pass --ssh (or -i <key>) to use the SSH path instead: that pushes your
public key into the sandbox, starts sshd, opens a tunnel, and hands
you off to system 'ssh'. Useful if you want OpenSSH features (agent
forwarding, ProxyJump, etc.).

Examples:
  createos sandbox shell                       # pick from list, keyless PTY
  createos sandbox shell my-box                # keyless PTY
  createos sandbox shell my-box --ssh          # SSH path (auto-detect ~/.ssh)
  createos sandbox shell my-box -i ~/.ssh/id   # SSH with explicit key
  createos sandbox shell my-box --user app     # log in as a non-root user`,
		Flags: []cli.Flag{
			&cli.BoolFlag{
				Name:  "ssh",
				Usage: "Use the SSH path instead of the keyless API PTY (also implied by -i)",
			},
			&cli.StringFlag{
				Name:    "identity",
				Aliases: []string{"i"},
				Usage:   "Path to your SSH private key (only used with --ssh; defaults to ~/.ssh/id_ed25519, then id_rsa, then id_ecdsa)",
			},
			&cli.StringFlag{
				Name:    "user",
				Aliases: []string{"u"},
				Value:   "root",
				Usage:   "Username inside the sandbox (SSH path only — the keyless PTY always runs as the sandbox's default user)",
			},
		},
		Action: runShell,
	}
}

func runShell(c *cli.Context) error {
	client, ok := c.App.Metadata[api.SandboxClientKey].(*api.SandboxClient)
	if !ok {
		return fmt.Errorf("you're not signed in — run 'createos login' to get started")
	}
	ref := strings.TrimSpace(c.Args().First())
	var id string
	if ref == "" {
		if !terminal.IsInteractive() {
			return fmt.Errorf("please provide a sandbox ID or name\n\n  Example:\n    createos sandbox shell my-box")
		}
		pickedID, label, err := pickByStatus(c, client, "Shell into which sandbox?", "running")
		if err != nil {
			return err
		}
		if pickedID == "" {
			fmt.Println("Cancelled. Nothing happened.")
			return nil
		}
		id = pickedID
		ref = label
	} else {
		resolvedID, err := resolveSandboxRef(c.Context, client, ref)
		if err != nil {
			return err
		}
		id = resolvedID
	}

	// Default to the keyless PTY path — no SSH key, no sshd setup, just
	// the user's API token. The SSH-based path is opt-in: explicitly via
	// --ssh, or implicitly when the user names a key with -i.
	useSSH := c.Bool("ssh") || strings.TrimSpace(c.String("identity")) != ""
	if !useSSH {
		return runShellPTY(c, id, ref)
	}
	return runShellSSH(c, client, id, ref)
}

// runShellSSH installs an SSH key into the sandbox, starts sshd,
// tunnels through the control plane, and hands control to system 'ssh'
// for a real PTY. Opt-in path: only used when --ssh or -i is set.
func runShellSSH(c *cli.Context, client *api.SandboxClient, id, ref string) error {
	privPath, pubPath, err := resolveIdentity(c.String("identity"))
	if err != nil {
		return err
	}
	pubBytes, err := os.ReadFile(pubPath)
	if err != nil {
		return fmt.Errorf("could not read public key %s: %w", pubPath, err)
	}

	user := strings.TrimSpace(c.String("user"))
	if user == "" {
		user = "root"
	}

	// 1. Drop the pubkey into the sandbox's authorized_keys via the
	//    file API. sshd refuses keys unless ~/.ssh is 0700 and the
	//    file is 0600 — we chmod in the next step.
	authPath := authorizedKeysPath(user)
	if err := client.UploadFile(c.Context, id, authPath, bytesReader(pubBytes), int64(len(pubBytes))); err != nil {
		return fmt.Errorf("could not install your SSH key: %w", err)
	}

	// 2. Make the modes right + start sshd. The script tolerates the
	//    "Address already in use" exit when sshd is already running,
	//    and recreates /run/sshd which is on tmpfs and disappears
	//    each boot.
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
# Start sshd if :22 isn't already bound. /proc/net/tcp is everywhere;
# ss/netstat aren't.
if ! awk 'NR>1{print $2}' /proc/net/tcp /proc/net/tcp6 2>/dev/null | grep -qi ':0016$'; then
  /usr/sbin/sshd
fi
`, filepath.Dir(authPath), user)
	resp, err := client.ExecSandbox(c.Context, id, api.SandboxExecReq{
		Cmd:  "sh",
		Args: []string{"-c", prepScript},
	})
	if err != nil {
		return fmt.Errorf("could not prepare sshd: %w", err)
	}
	if resp.Result.ExitCode == 100 {
		return fmt.Errorf("the sandbox image doesn't have sshd installed — try a different rootfs (e.g. `--rootfs devbox:1`)")
	}
	if resp.Result.ExitCode != 0 {
		return fmt.Errorf("sshd prep failed: %s", strings.TrimSpace(resp.Result.Stderr))
	}

	// 3. Open a local TCP listener that bridges every accepted
	//    connection through the control plane to the sandbox's :22.
	ctx, cancel := context.WithCancel(c.Context)
	defer cancel()
	bridge, err := startTunnelBridge(ctx, c, id, 22)
	if err != nil {
		return fmt.Errorf("could not open tunnel to the sandbox: %w", err)
	}
	defer bridge.close()

	if err := waitForTCP(bridge.localAddr, 5*time.Second); err != nil {
		return fmt.Errorf("sshd did not start in time: %w", err)
	}

	// 4. Hand off to system ssh through the local tunnel for a real PTY.
	_, port, _ := net.SplitHostPort(bridge.localAddr)
	pterm.Println(pterm.Gray(fmt.Sprintf("  connecting to %s as %s…", refLabel(ref, id), user)))
	sshCmd := exec.Command(
		"ssh",
		"-p", port,
		"-i", privPath,
		// IdentitiesOnly stops ssh from offering keys from the agent;
		// IdentityAgent=none stops it from loading default identity
		// files (~/.ssh/id_ed25519 etc.) which may be passphrase-
		// protected and prompt on a TTY. Together they pin auth to
		// exactly the -i key we installed on the sandbox.
		"-o", "IdentitiesOnly=yes",
		"-o", "IdentityAgent=none",
		"-o", "StrictHostKeyChecking=no",
		"-o", "UserKnownHostsFile=/dev/null",
		"-o", "LogLevel=ERROR",
		"-t",
		"-l", user,
		"127.0.0.1",
	)
	sshCmd.Stdin = os.Stdin
	sshCmd.Stdout = os.Stdout
	sshCmd.Stderr = os.Stderr
	err = sshCmd.Run()
	if exitErr, ok := err.(*exec.ExitError); ok {
		os.Exit(exitErr.ExitCode())
	}
	return err
}

// ── Keyless PTY path ──────────────────────────────────────────────
//
// Talks to POST /v1/sandboxes/:id/shell. The server upgrades the HTTP
// connection to a raw byte pipe to the sandbox's PTY listener. We frame
// our half: 0x00 (data) + 4-byte BE length + bytes, or 0x01 (resize) +
// 4-byte BE length + u16 rows + u16 cols. The agent's stdout/stderr
// come back unframed.

const (
	ptyFrameData   = 0
	ptyFrameResize = 1
)

// runShellPTY puts the local terminal in raw mode and pumps a real PTY
// session that runs inside the sandbox. Auth is the API token only.
func runShellPTY(c *cli.Context, id, ref string) error {
	stdinFd := int(os.Stdin.Fd())
	if !term.IsTerminal(stdinFd) {
		return fmt.Errorf("shell needs a real terminal — re-run interactively, or pass --ssh for the SSH path")
	}

	ctrlURL := strings.TrimSpace(c.String("sandbox-api-url"))
	if ctrlURL == "" {
		ctrlURL = api.DefaultSandboxBaseURL
	}
	token, err := loadAPIToken()
	if err != nil {
		return err
	}

	conn, err := dialControlUpgrade(c.Context, ctrlURL, token, "/v1/sandboxes/"+id+"/shell")
	if err != nil {
		return err
	}
	defer conn.Close()

	pterm.Println(pterm.Gray(fmt.Sprintf("  connecting to %s…", refLabel(ref, id))))

	oldState, err := term.MakeRaw(stdinFd)
	if err != nil {
		return fmt.Errorf("could not switch terminal to raw mode: %w", err)
	}
	defer func() { _ = term.Restore(stdinFd, oldState) }()

	// frameMu serialises writes to conn — the stdin pump and the
	// SIGWINCH handler both emit frames, and an interleaved write would
	// corrupt one of them.
	var frameMu sync.Mutex
	sendResize(conn, &frameMu, stdinFd)

	winch := make(chan os.Signal, 1)
	signal.Notify(winch, syscall.SIGWINCH)
	defer signal.Stop(winch)
	go func() {
		for range winch {
			sendResize(conn, &frameMu, stdinFd)
		}
	}()

	done := make(chan struct{}, 2)
	// remote → local screen
	go func() {
		_, _ = io.Copy(os.Stdout, conn)
		done <- struct{}{}
	}()
	// local keystrokes → framed stdin
	go func() {
		buf := make([]byte, 4096)
		for {
			n, rerr := os.Stdin.Read(buf)
			if n > 0 {
				if werr := writeFrame(conn, &frameMu, ptyFrameData, buf[:n]); werr != nil {
					break
				}
			}
			if rerr != nil {
				break
			}
		}
		done <- struct{}{}
	}()
	<-done
	return nil
}

// sendResize reads the current terminal size and emits a resize frame.
func sendResize(conn io.Writer, mu *sync.Mutex, fd int) {
	cols, rows, err := term.GetSize(fd)
	if err != nil {
		return
	}
	var p [4]byte
	binary.BigEndian.PutUint16(p[0:2], uint16(rows))
	binary.BigEndian.PutUint16(p[2:4], uint16(cols))
	_ = writeFrame(conn, mu, ptyFrameResize, p[:])
}

// writeFrame emits one [type:1][len:4 BE][payload] frame under mu.
func writeFrame(w io.Writer, mu *sync.Mutex, typ byte, payload []byte) error {
	var hdr [5]byte
	hdr[0] = typ
	binary.BigEndian.PutUint32(hdr[1:5], uint32(len(payload)))
	mu.Lock()
	defer mu.Unlock()
	if _, err := w.Write(hdr[:]); err != nil {
		return err
	}
	if len(payload) > 0 {
		if _, err := w.Write(payload); err != nil {
			return err
		}
	}
	return nil
}

// dialControlUpgrade dials the control plane, performs an HTTP/1.1
// Upgrade handshake, and returns the raw connection on a 101 reply.
// Used by the keyless PTY path — same wire shape as the tunnel bridge
// but with a different target path.
func dialControlUpgrade(ctx context.Context, ctrlURL, token, path string) (net.Conn, error) {
	u, err := url.Parse(ctrlURL)
	if err != nil {
		return nil, fmt.Errorf("bad sandbox URL %q: %w", ctrlURL, err)
	}
	host := u.Host
	var conn net.Conn
	d := &net.Dialer{Timeout: 10 * time.Second}
	if u.Scheme == "https" {
		if !strings.Contains(host, ":") {
			host += ":443"
		}
		sni, _, _ := net.SplitHostPort(host)
		conn, err = tls.DialWithDialer(d, "tcp", host, &tls.Config{
			ServerName: sni,
			NextProtos: []string{"http/1.1"},
		})
	} else {
		if !strings.Contains(host, ":") {
			host += ":80"
		}
		conn, err = d.DialContext(ctx, "tcp", host)
	}
	if err != nil {
		return nil, fmt.Errorf("dial %s: %w", host, err)
	}

	req := "POST " + path + " HTTP/1.1\r\n" +
		"Host: " + u.Host + "\r\n" +
		"X-Api-Key: " + token + "\r\n" +
		"Connection: Upgrade\r\n" +
		"Upgrade: tcp-tunnel\r\n" +
		"Content-Length: 0\r\n\r\n"
	if _, err := conn.Write([]byte(req)); err != nil {
		conn.Close()
		return nil, err
	}

	br := bufio.NewReader(conn)
	status, err := br.ReadString('\n')
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("read upgrade response: %w", err)
	}
	if !strings.Contains(status, " 101 ") {
		// Read up to a few KB so we can show the server's error message.
		body, _ := io.ReadAll(io.LimitReader(br, 4096))
		conn.Close()
		msg := strings.TrimSpace(string(body))
		if msg == "" {
			msg = strings.TrimSpace(status)
		}
		return nil, fmt.Errorf("could not open shell: %s", msg)
	}
	for {
		line, err := br.ReadString('\n')
		if err != nil {
			conn.Close()
			return nil, fmt.Errorf("read upgrade headers: %w", err)
		}
		if line == "\r\n" || line == "\n" {
			break
		}
	}
	return &bufferedConn{Conn: conn, r: br}, nil
}

// authorizedKeysPath gives the user's authorized_keys location.
// Special-cases root because /root != /home/root.
func authorizedKeysPath(user string) string {
	if user == "" || user == "root" {
		return "/root/.ssh/authorized_keys"
	}
	return "/home/" + user + "/.ssh/authorized_keys"
}

// ── Local TCP listener that bridges through the control plane ────
//
// fcctl calls this OpenTunnelsHTTP; same idea here, slimmed for our
// single use case (one local listener, one remote port).

type tunnelBridge struct {
	localAddr string // 127.0.0.1:<port>
	listener  net.Listener
	stop      context.CancelFunc
}

func (b *tunnelBridge) close() {
	if b == nil {
		return
	}
	if b.stop != nil {
		b.stop()
	}
	if b.listener != nil {
		_ = b.listener.Close()
	}
}

func startTunnelBridge(parent context.Context, c *cli.Context, sandboxID string, remotePort int) (*tunnelBridge, error) {
	ctrlURL := strings.TrimSpace(c.String("sandbox-api-url"))
	if ctrlURL == "" {
		ctrlURL = api.DefaultSandboxBaseURL
	}
	token, err := loadAPIToken()
	if err != nil {
		return nil, err
	}
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("local listen: %w", err)
	}
	ctx, cancel := context.WithCancel(parent)
	b := &tunnelBridge{
		localAddr: l.Addr().String(),
		listener:  l,
		stop:      cancel,
	}
	go func() {
		for {
			conn, err := l.Accept()
			if err != nil {
				return
			}
			go bridgeOne(ctx, ctrlURL, token, sandboxID, remotePort, conn)
		}
	}()
	return b, nil
}

// loadAPIToken pulls the user's token the same way the root Before
// hook does — OAuth session if present, else the static api-key file.
// We re-read because the Resty client doesn't expose the raw value.
func loadAPIToken() (string, error) {
	if config.HasOAuthSession() {
		sess, err := config.LoadOAuthSession()
		if err == nil && sess != nil && sess.AccessToken != "" {
			return sess.AccessToken, nil
		}
	}
	return config.LoadToken()
}

// bridgeOne handles a single accepted local connection: it opens an
// HTTP Upgrade to the sandbox-tunnel endpoint and splices bytes both
// ways until both directions have closed. We must wait for BOTH —
// not just one — because SSH negotiation interleaves writes from both
// peers, and closing early on a transient half-drain truncates the
// handshake mid-flight and looks like an auth failure.
func bridgeOne(ctx context.Context, ctrlURL, token, id string, port int, local net.Conn) {
	defer local.Close()
	remote, err := dialControlTunnel(ctx, ctrlURL, token, id, port)
	if err != nil {
		return
	}
	defer remote.Close()
	// closeWrite when one direction reaches EOF so the peer sees a
	// proper half-close instead of either side hanging on the read.
	// We still wait for the other goroutine before returning.
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		_, _ = io.Copy(remote, local)
		if cw, ok := remote.(interface{ CloseWrite() error }); ok {
			_ = cw.CloseWrite()
		}
	}()
	go func() {
		defer wg.Done()
		_, _ = io.Copy(local, remote)
		if cw, ok := local.(interface{ CloseWrite() error }); ok {
			_ = cw.CloseWrite()
		}
	}()
	doneCh := make(chan struct{})
	go func() { wg.Wait(); close(doneCh) }()
	select {
	case <-doneCh:
	case <-ctx.Done():
	}
}

// dialControlTunnel speaks the control plane's HTTP/1.1 Upgrade
// protocol by hand: POST `/v1/sandboxes/:id/tunnel/:port` with the
// Upgrade headers, watch for 101 Switching Protocols, then return a
// net.Conn that carries only tunnel bytes from then on.
func dialControlTunnel(ctx context.Context, ctrlURL, token, id string, port int) (net.Conn, error) {
	u, err := url.Parse(ctrlURL)
	if err != nil {
		return nil, fmt.Errorf("bad sandbox URL %q: %w", ctrlURL, err)
	}
	host := u.Host
	var conn net.Conn
	d := &net.Dialer{Timeout: 10 * time.Second}
	if u.Scheme == "https" {
		if !strings.Contains(host, ":") {
			host += ":443"
		}
		sni, _, _ := net.SplitHostPort(host)
		// Force HTTP/1.1 via ALPN. HTTP/2 doesn't expose the
		// hop-by-hop `Upgrade` header we rely on; if the server picks
		// h2 the tunnel handshake silently falls apart.
		conn, err = tls.DialWithDialer(d, "tcp", host, &tls.Config{
			ServerName: sni,
			NextProtos: []string{"http/1.1"},
		})
	} else {
		if !strings.Contains(host, ":") {
			host += ":80"
		}
		conn, err = d.DialContext(ctx, "tcp", host)
	}
	if err != nil {
		return nil, fmt.Errorf("dial %s: %w", host, err)
	}

	req := fmt.Sprintf("POST /v1/sandboxes/%s/tunnel/%d HTTP/1.1\r\n"+
		"Host: %s\r\nX-Api-Key: %s\r\n"+
		"Connection: Upgrade\r\nUpgrade: tcp-tunnel\r\nContent-Length: 0\r\n\r\n",
		id, port, u.Host, token)
	if _, err := conn.Write([]byte(req)); err != nil {
		conn.Close()
		return nil, err
	}

	br := bufio.NewReader(conn)
	status, err := br.ReadString('\n')
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("read tunnel response: %w", err)
	}
	if !strings.Contains(status, " 101 ") {
		conn.Close()
		return nil, fmt.Errorf("server rejected the tunnel: %s", strings.TrimSpace(status))
	}
	for {
		line, err := br.ReadString('\n')
		if err != nil {
			conn.Close()
			return nil, fmt.Errorf("read tunnel headers: %w", err)
		}
		if line == "\r\n" || line == "\n" {
			break
		}
	}
	return &bufferedConn{Conn: conn, r: br}, nil
}

// bufferedConn lets us hand back a conn whose first bytes were buffered
// by the bufio.Reader during the HTTP Upgrade handshake. Without this,
// any tunnel bytes pulled into the buffer get lost.
type bufferedConn struct {
	net.Conn
	r *bufio.Reader
}

func (b *bufferedConn) Read(p []byte) (int, error) { return b.r.Read(p) }

// ── tiny helpers ────────────────────────────────────────────────────

func waitForTCP(addr string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		c, err := net.DialTimeout("tcp", addr, 250*time.Millisecond)
		if err == nil {
			_ = c.Close()
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("%s did not start listening within %s", addr, timeout)
}

// bytesReader avoids dragging bytes.NewReader into every call site.
func bytesReader(b []byte) io.Reader {
	return &byteSliceReader{data: b}
}

type byteSliceReader struct {
	data []byte
	pos  int
}

func (r *byteSliceReader) Read(p []byte) (int, error) {
	if r.pos >= len(r.data) {
		return 0, io.EOF
	}
	n := copy(p, r.data[r.pos:])
	r.pos += n
	return n, nil
}

// resolveIdentity returns (private-key path, public-key path). If the
// user passed --identity we use that and look for a `.pub` next to it.
// Otherwise we try the canonical files in `~/.ssh/` in order.
func resolveIdentity(explicit string) (priv, pub string, err error) {
	if explicit != "" {
		priv = expandHome(explicit)
		pub = priv + ".pub"
		if _, e := os.Stat(priv); e != nil {
			return "", "", fmt.Errorf("could not find SSH private key %s", priv)
		}
		if _, e := os.Stat(pub); e != nil {
			return "", "", fmt.Errorf("could not find public-key file %s (expected next to the private key)", pub)
		}
		return priv, pub, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", "", fmt.Errorf("could not resolve $HOME: %w", err)
	}
	for _, name := range []string{"id_ed25519", "id_rsa", "id_ecdsa"} {
		p := filepath.Join(home, ".ssh", name)
		pp := p + ".pub"
		if _, e := os.Stat(p); e == nil {
			if _, e := os.Stat(pp); e == nil {
				return p, pp, nil
			}
		}
	}
	return "", "", fmt.Errorf("no SSH key found in ~/.ssh/ — generate one with `ssh-keygen -t ed25519`, or pass --identity <path>")
}

// expandHome turns a leading "~" into the user's home directory.
func expandHome(p string) string {
	if strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, p[2:])
		}
	}
	return p
}
