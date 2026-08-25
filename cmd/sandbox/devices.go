package sandbox

import (
	"context"
	"encoding/json"
	"errors"
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
	if mkErr := os.MkdirAll(filepath.Dir(p), 0o700); mkErr != nil {
		return mkErr
	}
	// gosec G117: this struct intentionally serializes the device's WG
	// private key — stored at 0o600 in the user's config dir; the server
	// never sees it. Encrypting at rest is out of scope for v1.
	b, err := json.MarshalIndent(st, "", "  ") //nolint:gosec
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
func genWGKeypair(ctx context.Context) (priv, pub string, err error) {
	if _, lookErr := exec.LookPath("wg"); lookErr != nil {
		fmt.Fprintln(os.Stderr, wgInstallHint)
		return "", "", errNoWG
	}
	out, err := exec.CommandContext(ctx, "wg", "genkey").Output()
	if err != nil {
		return "", "", fmt.Errorf("wg genkey: %w", err)
	}
	priv = strings.TrimSpace(string(out))

	cmd := exec.CommandContext(ctx, "wg", "pubkey")
	cmd.Stdin = strings.NewReader(priv + "\n")
	out, err = cmd.Output()
	if err != nil {
		return "", "", fmt.Errorf("wg pubkey: %w", err)
	}
	pub = strings.TrimSpace(string(out))
	return priv, pub, nil
}

// errNoWG is a sentinel returned when wireguard-tools isn't on PATH.
// The user-facing install hints are kept off the error string (revive's
// error-strings rule rejects multi-line/punctuated error text) and
// printed by callers via wgInstallHint().
var errNoWG = errors.New("wireguard-tools not installed")

// wgInstallHint is the multi-line install snippet for the platforms we
// care about. Printed alongside errNoWG so users see actionable advice
// without baking it into the error string.
const wgInstallHint = `wireguard-tools is required for VPN commands.

  macOS:    brew install wireguard-tools
  Ubuntu:   sudo apt install -y wireguard-tools
  Fedora:   sudo dnf install -y wireguard-tools

After installing, re-run this command.`

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
			newDevicesListCommand(),
			newDevicesRemoveCommand(),
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
	existing, _ := loadDeviceState() //nolint:errcheck // missing/corrupt file = not registered, fall through
	if existing != nil {
		pterm.Warning.Printfln("This machine is already registered as %q (%s).", existing.Name, existing.ClientIP)
		pterm.Println(pterm.Gray("  To re-register, first run: createos sb devices unregister"))
		return nil
	}
	hostname, _ := os.Hostname() //nolint:errcheck // hostname is optional metadata
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

	priv, pub, err := genWGKeypair(c.Context)
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
		pterm.Warning.Printfln("Server-side revoke failed: %s", api.UserMessageVerbose(err))
		pterm.Println(pterm.Gray("  (clearing local state anyway; revoke from the UI to be safe)"))
	}
	if err := clearDeviceState(); err != nil {
		return fmt.Errorf("clear local state: %w", err)
	}
	pterm.Success.Printfln("Unregistered %q.", st.Name)
	return nil
}

func newDevicesListCommand() *cli.Command {
	return &cli.Command{
		Name:    "list",
		Aliases: []string{"ls"},
		Usage:   "List your registered devices (all machines, not just this one)",
		Action:  runDeviceList,
	}
}

func runDeviceList(c *cli.Context) error {
	client, ok := c.App.Metadata[api.SandboxClientKey].(*api.SandboxClient)
	if !ok {
		return fmt.Errorf("you're not signed in — run 'createos login' to get started")
	}
	ctx, cancel := context.WithTimeout(c.Context, 10*time.Second)
	defer cancel()
	devs, err := client.ListDevices(ctx)
	if err != nil {
		return err
	}
	if len(devs) == 0 {
		pterm.Info.Println("No devices registered.")
		pterm.Println(pterm.Gray("  Register this machine with: createos sb devices register"))
		return nil
	}
	local, _ := loadDeviceState() //nolint:errcheck
	rows := make([][]string, 0, len(devs)+1)
	rows = append(rows, []string{"NAME", "IP", "OS", "ID", ""})
	for _, d := range devs {
		mark := ""
		if local != nil && local.DeviceID == d.ID {
			mark = "(this machine)"
		}
		rows = append(rows, []string{d.Name, d.ClientIP, d.OS, d.ID, mark})
	}
	return output.RenderTable(rows)
}

func newDevicesRemoveCommand() *cli.Command {
	return &cli.Command{
		Name:      "remove",
		Aliases:   []string{"rm"},
		Usage:     "Remove any device by id or name (server-side — use when you no longer have local state)",
		ArgsUsage: "[<id-or-name>]",
		Flags: []cli.Flag{
			&cli.BoolFlag{Name: "yes", Aliases: []string{"y"}, Usage: "skip confirmation"},
		},
		Action: runDeviceRemove,
	}
}

func runDeviceRemove(c *cli.Context) error {
	client, ok := c.App.Metadata[api.SandboxClientKey].(*api.SandboxClient)
	if !ok {
		return fmt.Errorf("you're not signed in — run 'createos login' to get started")
	}
	ctx, cancel := context.WithTimeout(c.Context, 15*time.Second)
	defer cancel()

	devs, err := client.ListDevices(ctx)
	if err != nil {
		return err
	}
	if len(devs) == 0 {
		pterm.Info.Println("No devices registered.")
		return nil
	}

	arg := strings.TrimSpace(c.Args().First())
	var target *api.DeviceView
	if arg != "" {
		for i := range devs {
			if devs[i].ID == arg || devs[i].Name == arg {
				target = &devs[i]
				break
			}
		}
		if target == nil {
			return fmt.Errorf("no device with id or name %q", arg)
		}
	} else {
		options := make([]string, 0, len(devs))
		byOpt := make(map[string]*api.DeviceView, len(devs))
		local, _ := loadDeviceState() //nolint:errcheck
		for i := range devs {
			d := &devs[i]
			mark := ""
			if local != nil && local.DeviceID == d.ID {
				mark = "  (this machine)"
			}
			opt := fmt.Sprintf("%s   (%s, id: %s)%s", d.Name, d.ClientIP, d.ID, mark)
			options = append(options, opt)
			byOpt[opt] = d
		}
		picked, selErr := pterm.DefaultInteractiveSelect.
			WithOptions(options).
			WithDefaultText("Remove which device?").
			Show()
		if selErr != nil {
			return fmt.Errorf("could not read your selection: %w", selErr)
		}
		target = byOpt[picked]
		if target == nil {
			return nil
		}
	}

	if !c.Bool("yes") {
		ok, confirmErr := pterm.DefaultInteractiveConfirm.
			WithDefaultText(fmt.Sprintf("Remove device %q (%s)? Active VPN sessions will drop.", target.Name, target.ClientIP)).
			Show()
		if confirmErr != nil {
			return fmt.Errorf("could not read your response: %w", confirmErr)
		}
		if !ok {
			pterm.Println(pterm.Gray("Aborted."))
			return nil
		}
	}

	if err := client.DeleteDevice(ctx, target.ID); err != nil {
		return err
	}
	pterm.Success.Printfln("Removed %q (%s).", target.Name, target.ClientIP)

	// If we just deleted the row backing this machine's device.json,
	// scrub local state too so `vpn up` doesn't loop against a ghost id.
	if local, _ := loadDeviceState(); local != nil && local.DeviceID == target.ID { //nolint:errcheck
		_ = clearDeviceState() //nolint:errcheck
		pterm.Println(pterm.Gray("  (also cleared local device.json for this machine)"))
	}
	return nil
}

var _ = output.Render
