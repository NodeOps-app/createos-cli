package sandbox

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/pterm/pterm"
	"github.com/urfave/cli/v2"

	"github.com/NodeOps-app/createos-cli/internal/api"
	"github.com/NodeOps-app/createos-cli/internal/output"
)

// deviceState is what we persist locally after `devices register`. The
// private key is the secret half of the WG keypair — server only ever
// sees the pubkey. Keeping it local means even a control-plane breach
// can't read traffic that flowed through us.
type deviceState struct {
	DeviceID   string `json:"device_id"`
	Name       string `json:"name"`
	ClientIP   string `json:"client_ip"`
	PrivateKey string `json:"private_key"` // base64
	Pubkey     string `json:"pubkey"`      // base64
	Hostname   string `json:"hostname,omitempty"`
	Server     string `json:"server,omitempty"`
}

func deviceStatePath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "createos", "device.json"), nil
}

func loadDeviceState() (*deviceState, error) {
	p, err := deviceStatePath()
	if err != nil {
		return nil, err
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return nil, err
	}
	var st deviceState
	if err := json.Unmarshal(b, &st); err != nil {
		return nil, err
	}
	return &st, nil
}

func saveDeviceState(st deviceState) error {
	p, err := deviceStatePath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p, b, 0o600)
}

func clearDeviceState() error {
	p, err := deviceStatePath()
	if err != nil {
		return err
	}
	if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// genWGKeypair shells out to `wg genkey | wg pubkey`. Requires
// wireguard-tools installed — the same prerequisite as `vpn up`.
func genWGKeypair() (priv, pub string, err error) {
	if _, err := exec.LookPath("wg"); err != nil {
		return "", "", errNoWG
	}
	out, err := exec.Command("wg", "genkey").Output()
	if err != nil {
		return "", "", fmt.Errorf("wg genkey: %w", err)
	}
	priv = strings.TrimSpace(string(out))

	cmd := exec.Command("wg", "pubkey")
	cmd.Stdin = strings.NewReader(priv + "\n")
	out, err = cmd.Output()
	if err != nil {
		return "", "", fmt.Errorf("wg pubkey: %w", err)
	}
	pub = strings.TrimSpace(string(out))
	return priv, pub, nil
}

// errNoWG carries the install instructions shown when wireguard-tools
// isn't on PATH. Used by both register (key gen) and vpn up (wg-quick).
var errNoWG = fmt.Errorf(`wireguard-tools not installed.

  macOS:    brew install wireguard-tools
  Ubuntu:   sudo apt install -y wireguard-tools
  Fedora:   sudo dnf install -y wireguard-tools

After installing, re-run this command.`)

// newDevicesCommand returns the `sb devices` group. Devices are this
// machine's identity to the network — register once, then `vpn up`
// brings up an encrypted tunnel into any of your attached networks.
func newDevicesCommand() *cli.Command {
	return &cli.Command{
		Name:    "devices",
		Aliases: []string{"device"},
		Usage:   "Register this machine to reach your private networks over VPN",
		Subcommands: []*cli.Command{
			newDevicesRegisterCommand(),
			newDevicesUnregisterCommand(),
		},
	}
}

func newDevicesRegisterCommand() *cli.Command {
	return &cli.Command{
		Name:      "register",
		Usage:     "Register this machine. One-time per laptop/desktop.",
		ArgsUsage: "[<name>]",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "name", Usage: "human-readable device name (default: machine hostname)"},
		},
		Action: runDeviceRegister,
	}
}

func runDeviceRegister(c *cli.Context) error {
	client, ok := c.App.Metadata[api.SandboxClientKey].(*api.SandboxClient)
	if !ok {
		return fmt.Errorf("you're not signed in — run 'createos login' to get started")
	}
	// Already registered? Surface that instead of silently double-registering.
	if existing, _ := loadDeviceState(); existing != nil {
		pterm.Warning.Printfln("This machine is already registered as %q (%s).", existing.Name, existing.ClientIP)
		pterm.Println(pterm.Gray("  To re-register, first run: createos sb devices unregister"))
		return nil
	}
	hostname, _ := os.Hostname()
	name := strings.TrimSpace(c.String("name"))
	if name == "" {
		name = strings.TrimSpace(c.Args().First())
	}
	if name == "" {
		name = hostname
	}
	if name == "" {
		return fmt.Errorf("please give this device a name:\n\n  createos sb devices register <name>")
	}

	priv, pub, err := genWGKeypair()
	if err != nil {
		return err
	}

	view, err := client.CreateDevice(c.Context, api.DeviceCreateReq{
		Name:     name,
		Pubkey:   pub,
		Hostname: hostname,
		OS:       runtime.GOOS,
	})
	if err != nil {
		return err
	}
	st := deviceState{
		DeviceID:   view.ID,
		Name:       view.Name,
		ClientIP:   view.ClientIP,
		PrivateKey: priv,
		Pubkey:     pub,
		Hostname:   hostname,
	}
	if err := saveDeviceState(st); err != nil {
		return fmt.Errorf("could not save device state: %w", err)
	}
	pterm.Success.Printfln("Registered %q (%s)", view.Name, view.ClientIP)
	pterm.Println(pterm.Gray("  Attach this device to a network in the UI, then:"))
	pterm.Println(pterm.Gray("    createos sb vpn up"))
	return nil
}

func newDevicesUnregisterCommand() *cli.Command {
	return &cli.Command{
		Name:   "unregister",
		Usage:  "Remove this machine from your devices",
		Action: runDeviceUnregister,
	}
}

func runDeviceUnregister(c *cli.Context) error {
	client, ok := c.App.Metadata[api.SandboxClientKey].(*api.SandboxClient)
	if !ok {
		return fmt.Errorf("you're not signed in — run 'createos login' to get started")
	}
	st, err := loadDeviceState()
	if err != nil || st == nil {
		pterm.Info.Println("This machine isn't registered.")
		return nil
	}
	ctx, cancel := context.WithTimeout(c.Context, 10*time.Second)
	defer cancel()
	if err := client.DeleteDevice(ctx, st.DeviceID); err != nil {
		pterm.Warning.Printfln("Server-side revoke failed: %v", err)
		pterm.Println(pterm.Gray("  (clearing local state anyway; revoke from the UI to be safe)"))
	}
	if err := clearDeviceState(); err != nil {
		return fmt.Errorf("clear local state: %w", err)
	}
	pterm.Success.Printfln("Unregistered %q.", st.Name)
	return nil
}

// stub used by `createos sb devices ls` if you ever want it — silenced
// for now since the design specifies only register/unregister.
var _ = output.Render
