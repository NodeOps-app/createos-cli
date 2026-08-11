package sandbox

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/pterm/pterm"
	"github.com/urfave/cli/v2"

	"github.com/NodeOps-app/createos-cli/internal/api"
	"github.com/NodeOps-app/createos-cli/internal/terminal"
)

func newVPNCommand() *cli.Command {
	return &cli.Command{
		Name:  "vpn",
		Usage: "Open a WireGuard tunnel into your private networks",
		Subcommands: []*cli.Command{
			newVPNUpCommand(),
		},
	}
}

func newVPNUpCommand() *cli.Command {
	return &cli.Command{
		Name:   "up",
		Usage:  "Connect this machine to your networks. Stays up until you press Ctrl-C.",
		Action: runVPNUp,
	}
}

func runVPNUp(c *cli.Context) error {
	client, ok := c.App.Metadata[api.SandboxClientKey].(*api.SandboxClient)
	if !ok {
		return fmt.Errorf("you're not signed in — run 'createos login' to get started")
	}
	st, err := loadDeviceState()
	if err != nil || st == nil {
		return fmt.Errorf(`this machine isn't registered yet.

  Run once:
    createos sb devices register

  Then come back and:
    createos sb vpn up`)
	}
	if _, lookErr := exec.LookPath("wg-quick"); lookErr != nil {
		fmt.Fprintln(os.Stderr, wgInstallHint)
		return errNoWG
	}

	// Open a session up front so we fail fast if the API or relay are
	// down — better than running wg-quick and discovering it later.
	sessCtx, cancelSess := context.WithTimeout(c.Context, 10*time.Second)
	sess, err := client.CreateDeviceSession(sessCtx, st.DeviceID)
	cancelSess()
	if err != nil {
		return fmt.Errorf("could not open VPN session: %w", err)
	}

	// Splice the locally-held private key into the server-issued config.
	// The server's wg-quick config has no PrivateKey line (server never
	// holds it); inject ours right after the [Interface] header.
	conf := injectPrivateKey(sess.ClientConfig, st.PrivateKey)

	// Write the config to a temp file. wg-quick derives the kernel iface
	// name from the config's basename (sans .conf). Linux caps iface
	// names at 15 chars, and the name must match ^[a-zA-Z0-9_=+.-]+$,
	// so we use a short fixed name (single tunnel per machine).
	confPath := filepath.Join(os.TempDir(), "cosvpn.conf")
	tmp, err := os.Create(confPath)
	if err != nil {
		return fmt.Errorf("temp file: %w", err)
	}
	if _, wErr := tmp.WriteString(conf); wErr != nil {
		_ = tmp.Close()         //nolint:errcheck // cleanup; original error wins
		_ = os.Remove(confPath) //nolint:errcheck // cleanup; original error wins
		return fmt.Errorf("write config: %w", wErr)
	}
	_ = tmp.Close()                            //nolint:errcheck // close-after-write — flush already done
	defer func() { _ = os.Remove(confPath) }() //nolint:errcheck // best-effort cleanup of temp file

	debug := c.Bool("debug")

	// CGNAT / subnet conflict check. Our wg-quick config installs routes
	// for the device's CGNAT pool (100.64.0.0/10) plus the VM subnets the
	// device is authorised for. If any of those overlap with a route
	// already in the local routing table — e.g. the user is on Tailscale
	// (also 100.64.0.0/10), a corporate VPN with a 10.0.0.0/8 subnet, or
	// a home LAN happening to use 10.0.0.0/22 — installing OUR routes
	// would silently steal that traffic. Stop loudly instead.
	if conflict := detectRouteConflict(c.Context, conf); conflict != "" {
		_ = closeSessionBestEffort(client, st.DeviceID, sess.SessionID) //nolint:errcheck
		return fmt.Errorf(`route conflict detected: %s

  another VPN or local network is already using an IP range that
  overlaps with your createos tunnel. Bringing up the tunnel would
  steal that traffic. Disconnect the other VPN (e.g. tailscale down)
  or remove the conflicting route, then re-run 'createos sb vpn up'`, conflict)
	}

	// Detect a stale cosvpn iface left by a prior run (OOM, kernel panic,
	// force-quit). On macOS, wg-quick stores the utun→name mapping in
	// /var/run/wireguard/<name>.name — checking this file needs no sudo
	// (unlike `wg show`). On Linux, fall back to `wg show` (which works
	// without sudo on some distros). If it's up, ask before tearing it
	// down — silent removal could kill an intentional tunnel the user
	// set up manually.
	staleIface := false
	if _, statErr := os.Stat("/var/run/wireguard/cosvpn.name"); statErr == nil {
		staleIface = true
	} else if probe := exec.CommandContext(c.Context, "wg", "show", "cosvpn").Run(); probe == nil {
		staleIface = true
	}
	if staleIface {
		msg := "A WireGuard interface named 'cosvpn' is already up on this machine."
		if !terminal.IsInteractive() {
			return fmt.Errorf("%s Bring it down first with 'sudo wg-quick down cosvpn' and re-run", msg)
		}
		pterm.Warning.Println(msg)
		ok, cErr := pterm.DefaultInteractiveConfirm.
			WithDefaultText("Reset it and continue?").
			WithDefaultValue(false).
			Show()
		if cErr != nil {
			return fmt.Errorf("could not read confirmation: %w", cErr)
		}
		if !ok {
			return fmt.Errorf("cancelled — leaving existing cosvpn tunnel in place")
		}
		cleanup := sudoCommand(c.Context, "wg-quick", "down", confPath)
		var cleanupBuf bytes.Buffer
		cleanup.Stdout, cleanup.Stderr = pickWGOutputs(debug, &cleanupBuf)
		if cleanupErr := cleanup.Run(); cleanupErr != nil {
			// wg-quick down can fail if the config doesn't match the
			// running interface. Fall back to removing the utun device
			// directly via the name file that macOS wg-quick leaves in
			// /var/run/wireguard/.
			if nameBytes, rErr := os.ReadFile("/var/run/wireguard/cosvpn.name"); rErr == nil {
				utun := strings.TrimSpace(string(nameBytes))
				_ = sudoCommand(c.Context, "rm", "-f", "/var/run/wireguard/cosvpn.name").Run()   //nolint:errcheck
				_ = sudoCommand(c.Context, "rm", "-f", "/var/run/wireguard/"+utun+".sock").Run() //nolint:errcheck
				_ = sudoCommand(c.Context, "ifconfig", utun, "destroy").Run()                    //nolint:errcheck
			}
		}
	}

	// macOS: sweep stale utun leftovers before wg-quick brings up a
	// new interface. wg-quick on darwin uses userspace wireguard-go and
	// on hard shutdowns (kernel panic, force quit, laptop sleep) leaves
	// utunN.sock + route entries behind. New tunnels get a fresh utun
	// number, but the OLD utun's route for the sandbox subnet is still
	// installed → packets vanish into a dead interface.
	if runtime.GOOS == "darwin" {
		cleanupStaleWGUtuns(c.Context, debug)
	}

	// Bring the tunnel up. wg-quick echoes every shell command it runs
	// ("[#] wg setconf ...", "[#] ip route add ...") which is pure noise
	// for the happy path. Suppress unless --debug is set; on failure we
	// still want the captured output so the user can diagnose.
	upCmd := sudoCommand(c.Context, "wg-quick", "up", confPath)
	var upBuf bytes.Buffer
	upCmd.Stdout, upCmd.Stderr = pickWGOutputs(debug, &upBuf)
	if runErr := upCmd.Run(); runErr != nil {
		if !debug {
			_, _ = io.Copy(os.Stderr, &upBuf) //nolint:errcheck // diagnostic dump; original error wins
		}
		_ = closeSessionBestEffort(client, st.DeviceID, sess.SessionID) //nolint:errcheck // best-effort cleanup
		return fmt.Errorf("wg-quick up: %w", runErr)
	}

	ifaceName := strings.TrimSuffix(filepath.Base(confPath), ".conf")
	// Emitted before the block on Ctrl-C, for the same reason as the
	// tunnel: the caller needs the interface and address up front.
	renderResult(c, "vpn_connected", map[string]any{
		"device_id": st.DeviceID,
		"device":    st.Name,
		"client_ip": st.ClientIP,
		"interface": ifaceName,
	}, func() {
		pterm.Success.Printfln("VPN connected as %s (%s).", st.Name, st.ClientIP)
		pterm.Println(pterm.Gray(fmt.Sprintf("  device: %s", st.Name)))
		pterm.Println(pterm.Gray(fmt.Sprintf("  iface:  %s", ifaceName)))
		pterm.Println(pterm.Gray("Press Ctrl-C to disconnect."))
	})

	// Block until Ctrl-C / SIGTERM (user disconnect) or until the
	// renewal goroutine signals that the server-side session is gone.
	// Best-effort cleanup on the way out either way.
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)

	// Renewal goroutine: PUT /sessions/:id every ~TTL/2 so an active
	// tunnel keeps its server-side session alive. If the server returns
	// 404 (sweeper got it, admin revoked, etc), or if two consecutive
	// renews fail with network errors, we self-signal a teardown — the
	// local WG iface without a matching server session is silently
	// broken and we'd rather the user know than have it look-alive-but-
	// fail-quietly.
	const renewInterval = 30 * time.Second
	renewCtx, cancelRenew := context.WithCancel(c.Context)
	defer cancelRenew()
	go func() {
		t := time.NewTicker(renewInterval)
		defer t.Stop()
		misses := 0
		for {
			select {
			case <-renewCtx.Done():
				return
			case <-t.C:
				ctx2, cancel := context.WithTimeout(renewCtx, 10*time.Second)
				err := client.RenewDeviceSession(ctx2, st.DeviceID, sess.SessionID)
				cancel()
				if err == nil {
					misses = 0
					continue
				}
				if api.IsNotFound(err) {
					pterm.Warning.Printfln("session lost server-side — tearing down tunnel")
					select {
					case sig <- syscall.SIGTERM:
					default:
					}
					return
				}
				misses++
				pterm.Warning.Printfln("renew failed (%d/2): %v", misses, err)
				if misses >= 2 {
					pterm.Error.Println("renewal repeatedly failed — assuming server lost session")
					select {
					case sig <- syscall.SIGTERM:
					default:
					}
					return
				}
			}
		}
	}()

	<-sig

	pterm.Println()
	pterm.Info.Println("Disconnecting...")
	downCtx, cancelDown := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelDown()
	downCmd := sudoCommand(downCtx, "wg-quick", "down", confPath)
	var downBuf bytes.Buffer
	downCmd.Stdout, downCmd.Stderr = pickWGOutputs(debug, &downBuf)
	if runErr := downCmd.Run(); runErr != nil && !debug {
		_, _ = io.Copy(os.Stderr, &downBuf) //nolint:errcheck // diagnostic dump
	}
	if err := closeSessionBestEffort(client, st.DeviceID, sess.SessionID); err != nil {
		pterm.Warning.Printfln("Server-side session close failed: %v", err)
	}
	pterm.Success.Println("Disconnected.")
	return nil
}

// injectPrivateKey writes the client's locally-held private key into
// the server-issued wg-quick config. The server NEVER ships PrivateKey,
// so we append it as the last line of the [Interface] section.
func injectPrivateKey(config, privkey string) string {
	const marker = "[Interface]"
	idx := strings.Index(config, marker)
	if idx < 0 {
		// Defensive — server should always include [Interface]. Prepend
		// as a fallback so wg-quick at least parses.
		return fmt.Sprintf("[Interface]\nPrivateKey = %s\n%s", privkey, config)
	}
	insertAt := idx + len(marker)
	return config[:insertAt] + "\nPrivateKey = " + privkey + config[insertAt:]
}

// pickWGOutputs returns the (stdout, stderr) wiring for a wg-quick child.
// debug=true tees both through to the user's terminal; debug=false captures
// into buf so we can replay the diagnostic only on failure.
func pickWGOutputs(debug bool, buf *bytes.Buffer) (io.Writer, io.Writer) {
	if debug {
		return os.Stdout, os.Stderr
	}
	return buf, buf
}

// sudoCommand wraps wg-quick with sudo on non-Windows. wg-quick needs
// CAP_NET_ADMIN to manage the kernel iface; running as a normal user
// always fails. Wrapping in sudo here is friendlier than telling the
// user to run the whole `createos` command as root.
//
// gosec G204: name/args are hard-coded callsites from this package
// ("wg-quick up <confPath>" / "wg-quick down <confPath>") where confPath
// is generated inside vpn.go from os.TempDir() + a constant basename —
// no user input flows into either field.
func sudoCommand(ctx context.Context, name string, args ...string) *exec.Cmd {
	if _, err := exec.LookPath("sudo"); err == nil {
		full := append([]string{name}, args...)
		return exec.CommandContext(ctx, "sudo", full...) //nolint:gosec
	}
	return exec.CommandContext(ctx, name, args...) //nolint:gosec
}

func closeSessionBestEffort(client *api.SandboxClient, deviceID, sessionID string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return client.DeleteDeviceSession(ctx, deviceID, sessionID)
}

// detectRouteConflict checks whether any of the AllowedIPs in our
// pending wg-quick config overlaps with a route already in the local
// routing table that points at a DIFFERENT interface. Returns a
// human-readable description of the first conflict found, or "" if none.
//
// Skips loopback + own-iface routes. "ip" missing or output unparseable
// → returns "" (best-effort; we'd rather let wg-quick try and surface
// its own error than block on a tooling absence).
func detectRouteConflict(ctx context.Context, conf string) string {
	allowed := parseAllowedIPs(conf)
	if len(allowed) == 0 {
		return ""
	}
	existing := listLocalRoutes(ctx)
	if len(existing) == 0 {
		return ""
	}
	for _, ours := range allowed {
		for _, theirs := range existing {
			if theirs.iface == "cosvpn" || theirs.iface == "lo" {
				continue // our own iface (stale from prior run) or loopback
			}
			if cidrsOverlap(ours, theirs.dst) {
				return fmt.Sprintf("%s (ours) overlaps %s on dev %s",
					ours.String(), theirs.dst.String(), theirs.iface)
			}
		}
	}
	return ""
}

// parseAllowedIPs pulls every CIDR from the [Peer] AllowedIPs line(s)
// of a wg-quick config. Robust to comma-separated and multi-line forms.
func parseAllowedIPs(conf string) []*net.IPNet {
	var out []*net.IPNet
	for _, line := range strings.Split(conf, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(strings.ToLower(line), "allowedips") {
			continue
		}
		eq := strings.IndexByte(line, '=')
		if eq < 0 {
			continue
		}
		for _, item := range strings.Split(line[eq+1:], ",") {
			item = strings.TrimSpace(item)
			if item == "" {
				continue
			}
			if _, cidr, err := net.ParseCIDR(item); err == nil {
				out = append(out, cidr)
			}
		}
	}
	return out
}

type localRoute struct {
	dst   *net.IPNet
	iface string
}

// listLocalRoutes shells out to `ip -j route show` and parses the JSON.
// `ip` is part of iproute2 — available everywhere wg-quick runs (Linux
// + Homebrew on macOS via `iproute2mac` — though macOS doesn't actually
// have `ip` by default, so on macOS this returns nil and the check is
// effectively a no-op there. wg-quick's own conflict detection takes
// over.)
func listLocalRoutes(ctx context.Context) []localRoute {
	out, err := exec.CommandContext(ctx, "ip", "-j", "route", "show").Output()
	if err != nil {
		return nil
	}
	var raw []struct {
		Dst string `json:"dst"`
		Dev string `json:"dev"`
	}
	if err := json.Unmarshal(out, &raw); err != nil {
		return nil
	}
	routes := make([]localRoute, 0, len(raw))
	for _, r := range raw {
		if r.Dst == "" || r.Dst == "default" {
			continue
		}
		// `ip -j` emits a bare IP for /32 routes; tack on the mask so
		// ParseCIDR is happy.
		dstStr := r.Dst
		if !strings.Contains(dstStr, "/") {
			if ip := net.ParseIP(dstStr); ip != nil {
				if ip.To4() != nil {
					dstStr += "/32"
				} else {
					dstStr += "/128"
				}
			}
		}
		_, cidr, err := net.ParseCIDR(dstStr)
		if err != nil {
			continue
		}
		routes = append(routes, localRoute{dst: cidr, iface: r.Dev})
	}
	return routes
}

// cidrsOverlap reports whether two networks share any IP. Either one
// being a superset of the other (or both equal) counts.
func cidrsOverlap(a, b *net.IPNet) bool {
	if a == nil || b == nil {
		return false
	}
	return a.Contains(b.IP) || b.Contains(a.IP)
}

// cleanupStaleWGUtuns removes leftover wireguard-go interfaces on macOS
// whose control socket exists at /var/run/wireguard/utunN.sock but the
// backing wireguard-go process is gone. Their routes stick around and
// steal traffic for the sandbox subnet, so a fresh wg-quick up ends up
// dropping packets into a dead interface. Runs `wg show` per candidate;
// on failure, tears down the routes it owns, destroys the utun, and
// removes the stale sock + name files.
func cleanupStaleWGUtuns(ctx context.Context, debug bool) {
	const wgRunDir = "/var/run/wireguard"
	// Only touch entries wg-quick manages. If the dir doesn't exist,
	// nothing to clean up.
	entries, err := os.ReadDir(wgRunDir)
	if err != nil {
		return
	}
	for _, e := range entries {
		name := e.Name()
		// utunN.sock only — skip *.name / anything else.
		if !strings.HasPrefix(name, "utun") || !strings.HasSuffix(name, ".sock") {
			continue
		}
		utun := strings.TrimSuffix(name, ".sock")
		// #nosec G204 -- utun name comes from a filename in the wg-controlled
		// /var/run/wireguard/ dir; the "utun" prefix + ".sock" suffix
		// checked above bound the shape further.
		if err := exec.CommandContext(ctx, "wg", "show", utun).Run(); err == nil {
			continue
		}
		// Dead → tear it down. Route deletes are best-effort; some may
		// not exist. On any error we keep going: worst case wg-quick up
		// fails and prints a helpful message.
		if debug {
			fmt.Fprintf(os.Stderr, "wg-quick preflight: cleaning stale %s\n", utun)
		}
		// Wipe any routes still pointing at this dead interface. macOS
		// stores them keyed by interface, so `route delete -interface`
		// nukes them without needing to enumerate every subnet.
		_ = sudoCommand(ctx, "route", "-n", "delete", "-inet", "-interface", utun).Run() //nolint:errcheck
		_ = sudoCommand(ctx, "ifconfig", utun, "destroy").Run()                          //nolint:errcheck
		_ = sudoCommand(ctx, "rm", "-f", filepath.Join(wgRunDir, utun+".sock")).Run()    //nolint:errcheck
	}
	// If cosvpn.name points at a utun that's now gone, drop the mapping
	// too so wg-quick up doesn't try to reuse a dead handle.
	if nameFile, err := os.ReadFile(filepath.Join(wgRunDir, "cosvpn.name")); err == nil {
		mapped := strings.TrimSpace(string(nameFile))
		if mapped != "" {
			if err := exec.CommandContext(ctx, "wg", "show", mapped).Run(); err != nil { //nolint:gosec // G204 false positive; mapped is a utun name read from wg-controlled /var/run/wireguard/
				_ = sudoCommand(ctx, "rm", "-f", filepath.Join(wgRunDir, "cosvpn.name")).Run() //nolint:errcheck
			}
		}
	}
}
