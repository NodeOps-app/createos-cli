package sandbox

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/pterm/pterm"
	"github.com/urfave/cli/v2"

	"github.com/NodeOps-app/createos-cli/internal/api"
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
		Name:  "up",
		Usage: "Connect this machine to your networks. Stays up until you press Ctrl-C.",
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
	if _, err := exec.LookPath("wg-quick"); err != nil {
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
	if _, err := tmp.WriteString(conf); err != nil {
		tmp.Close()
		os.Remove(confPath)
		return fmt.Errorf("write config: %w", err)
	}
	tmp.Close()
	defer os.Remove(confPath)

	// Bring the tunnel up. wg-quick prints noisy "ip route add" lines
	// to stderr; pipe them through so the user can see if something
	// fails (e.g., conflicting subnet routes).
	upCmd := sudoCommand("wg-quick", "up", confPath)
	upCmd.Stdout = os.Stdout
	upCmd.Stderr = os.Stderr
	if err := upCmd.Run(); err != nil {
		_ = closeSessionBestEffort(client, st.DeviceID, sess.SessionID)
		return fmt.Errorf("wg-quick up: %w", err)
	}

	ifaceName := strings.TrimSuffix(filepath.Base(confPath), ".conf")
	pterm.Success.Printfln("VPN connected as %s (%s).", st.Name, st.ClientIP)
	pterm.Println(pterm.Gray(fmt.Sprintf("  device: %s", st.Name)))
	pterm.Println(pterm.Gray(fmt.Sprintf("  iface:  %s", ifaceName)))
	pterm.Println(pterm.Gray("Press Ctrl-C to disconnect."))

	// Block until Ctrl-C / SIGTERM. Best-effort cleanup on the way out.
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	<-sig

	pterm.Println()
	pterm.Info.Println("Disconnecting...")
	downCmd := sudoCommand("wg-quick", "down", confPath)
	downCmd.Stdout = os.Stdout
	downCmd.Stderr = os.Stderr
	_ = downCmd.Run()
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

// sudoCommand wraps wg-quick with sudo on non-Windows. wg-quick needs
// CAP_NET_ADMIN to manage the kernel iface; running as a normal user
// always fails. Wrapping in sudo here is friendlier than telling the
// user to run the whole `createos` command as root.
func sudoCommand(name string, args ...string) *exec.Cmd {
	if _, err := exec.LookPath("sudo"); err == nil {
		full := append([]string{name}, args...)
		return exec.Command("sudo", full...)
	}
	return exec.Command(name, args...)
}

func closeSessionBestEffort(client *api.SandboxClient, deviceID, sessionID string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return client.DeleteDeviceSession(ctx, deviceID, sessionID)
}
