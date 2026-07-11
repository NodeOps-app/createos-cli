package sandbox

import (
	"fmt"
	"net"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"

	"github.com/pterm/pterm"
	"github.com/urfave/cli/v2"

	"github.com/NodeOps-app/createos-cli/internal/api"
	"github.com/NodeOps-app/createos-cli/internal/terminal"
)

// newTunnelCommand wires up `createos sandbox tunnel`.
//
// Forwards a local TCP port to a port inside a sandbox via the control
// plane's tunnel endpoint. No SSH keys, no gateway hop — your API token
// is the only auth.
//
// Example: a web server inside the sandbox bound to :8000 becomes
// reachable from your laptop at http://127.0.0.1:8080 with
//
//	createos sandbox tunnel my-box --local 8080 --remote 8000
func newTunnelCommand() *cli.Command {
	return &cli.Command{
		Name:      "tunnel",
		Aliases:   []string{"tun"},
		Usage:     "Forward a local port into a port inside a sandbox",
		ArgsUsage: "[<sandbox>]",
		Description: `Forwards a TCP port on your laptop to a port inside the sandbox.
Useful for reaching loopback-only services (e.g. a dev server bound to
127.0.0.1:3000) without an SSH key.

Examples:
  # Reach a Python http.server inside the sandbox at http://localhost:8080
  createos sandbox tunnel my-box --local 8080 --remote 8000

  # Default --local to the same as --remote
  createos sandbox tunnel my-box --remote 5432

  # Pick a sandbox interactively, then prompt for ports
  createos sandbox tunnel

  # Bind to 0.0.0.0 so other machines on the LAN can reach the tunnel
  createos sandbox tunnel my-box --remote 80 --bind 0.0.0.0

Press Ctrl+C to stop. The tunnel only lives for the lifetime of this
command — no daemon, no global state.`,
		Flags: []cli.Flag{
			&cli.IntFlag{
				Name:  "local",
				Usage: "Local port to listen on (defaults to --remote)",
			},
			&cli.IntFlag{
				Name:  "remote",
				Usage: "Port inside the sandbox to forward to",
			},
			&cli.StringFlag{
				Name:  "bind",
				Value: "127.0.0.1",
				Usage: "Local address to bind to (use 0.0.0.0 to expose on the LAN)",
			},
		},
		Action: runTunnel,
	}
}

func runTunnel(c *cli.Context) error {
	client, ok := c.App.Metadata[api.SandboxClientKey].(*api.SandboxClient)
	if !ok {
		return fmt.Errorf("you're not signed in — run 'createos login' to get started")
	}
	tty := terminal.IsInteractive()

	// 1. Sandbox + ports. Re-scan args by hand so flags work in any
	// position: urfave/cli v2 stops flag parsing at the first positional
	// (see parseEditArgs in edit.go), so `tunnel my-sb --remote 8000`
	// would otherwise drop --remote and fail "--remote <port> is required".
	ref, local, remote, bind := parseTunnelArgs(c)
	var id string
	if ref == "" {
		if !tty {
			return fmt.Errorf("please provide a sandbox ID or name\n\n  Example:\n    createos sandbox tunnel my-box --local 8080 --remote 8000")
		}
		pickedID, label, err := pickByStatus(c, client, "Tunnel to which sandbox?", "running")
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

	// 2. Ports. Prompt on TTY when missing.
	if remote <= 0 {
		if !tty {
			return fmt.Errorf("--remote <port> is required\n\n  Example:\n    createos sandbox tunnel %s --local 8080 --remote 8000", ref)
		}
		v, err := pterm.DefaultInteractiveTextInput.
			WithDefaultText("Remote port inside the sandbox").
			Show()
		if err != nil {
			return fmt.Errorf("could not read remote port: %w", err)
		}
		p, perr := strconv.Atoi(strings.TrimSpace(v))
		if perr != nil || p <= 0 || p > 65535 {
			return fmt.Errorf("remote port must be 1–65535")
		}
		remote = p
	}
	if local <= 0 {
		// On TTY ask; non-TTY just mirror the remote port.
		if tty {
			v, err := pterm.DefaultInteractiveTextInput.
				WithDefaultText(fmt.Sprintf("Local port to listen on (enter for %d)", remote)).
				WithDefaultValue(strconv.Itoa(remote)).
				Show()
			if err != nil {
				return fmt.Errorf("could not read local port: %w", err)
			}
			v = strings.TrimSpace(v)
			if v == "" {
				local = remote
			} else {
				p, perr := strconv.Atoi(v)
				if perr != nil || p <= 0 || p > 65535 {
					return fmt.Errorf("local port must be 1–65535")
				}
				local = p
			}
		} else {
			local = remote
		}
	}
	if bind == "" {
		bind = "127.0.0.1"
	}

	// 3. Open a TCP listener on (bind:local). Every accepted connection
	//    opens its own HTTP-Upgrade tunnel through control to the
	//    sandbox's `remote` port.
	listenAddr := net.JoinHostPort(bind, strconv.Itoa(local))
	var lc net.ListenConfig
	listener, err := lc.Listen(c.Context, "tcp", listenAddr)
	if err != nil {
		return fmt.Errorf("could not bind %s: %w", listenAddr, err)
	}
	defer func() { _ = listener.Close() }() //nolint:errcheck

	ctrlURL := strings.TrimSpace(c.String("sandbox-api-url"))
	if ctrlURL == "" {
		ctrlURL = api.DefaultSandboxBaseURL
	}
	authHeader, token, err := sandboxAuth(c)
	if err != nil {
		return err
	}

	pterm.Success.Printfln("Forwarding %s → %s:%d", listenAddr, refLabel(ref, id), remote)
	pterm.Println(pterm.Gray("  Press Ctrl+C to stop."))

	// Trap Ctrl+C so we can close cleanly and not leave half-open conns.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigCh)
	go func() {
		<-sigCh
		_ = listener.Close() //nolint:errcheck
	}()

	// 4. Accept loop. Each connection runs in its own goroutine via
	//    bridgeOne (defined in shell.go) which speaks the same
	//    HTTP-Upgrade tunnel protocol.
	for {
		conn, err := listener.Accept()
		if err != nil {
			// Closed by signal handler or local error → done.
			return nil
		}
		go bridgeOne(c.Context, ctrlURL, authHeader, token, id, remote, conn)
	}
}

// parseTunnelArgs re-scans the raw args so --local/--remote/--bind work in
// any position. urfave/cli v2 stops flag parsing at the first positional, so
// `tunnel my-sb --remote 8000` loses --remote otherwise (mirrors
// parseEditArgs in edit.go). Seeds from c.* first to keep flags placed before
// the positional working, then lets a later occurrence override.
func parseTunnelArgs(c *cli.Context) (ref string, local, remote int, bind string) {
	local = c.Int("local")
	remote = c.Int("remote")
	bind = strings.TrimSpace(c.String("bind"))

	atoi := func(s string) int { n, _ := strconv.Atoi(strings.TrimSpace(s)); return n }
	args := c.Args().Slice()
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--remote":
			if i+1 < len(args) {
				remote = atoi(args[i+1])
				i++
			}
		case strings.HasPrefix(a, "--remote="):
			remote = atoi(strings.TrimPrefix(a, "--remote="))
		case a == "--local":
			if i+1 < len(args) {
				local = atoi(args[i+1])
				i++
			}
		case strings.HasPrefix(a, "--local="):
			local = atoi(strings.TrimPrefix(a, "--local="))
		case a == "--bind":
			if i+1 < len(args) {
				bind = strings.TrimSpace(args[i+1])
				i++
			}
		case strings.HasPrefix(a, "--bind="):
			bind = strings.TrimSpace(strings.TrimPrefix(a, "--bind="))
		default:
			if ref == "" && !strings.HasPrefix(a, "-") {
				ref = strings.TrimSpace(a)
			}
		}
	}
	return ref, local, remote, bind
}
