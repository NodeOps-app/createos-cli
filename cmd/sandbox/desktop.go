package sandbox

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/pterm/pterm"
	"github.com/urfave/cli/v2"

	"github.com/NodeOps-app/createos-cli/internal/api"
	"github.com/NodeOps-app/createos-cli/internal/output"
	"github.com/NodeOps-app/createos-cli/internal/terminal"
)

// The desktop stack (Xvfb → XFCE → x11vnc → websockify) starts *after* the
// sandbox reports `running`, so every computer call fails for the first while.
// Nothing upstream polls for this, which means every caller ends up writing
// this wait — so the CLI does it once, here.
const (
	desktopReadyTimeout = 2 * time.Minute
	desktopPollInterval = 2 * time.Second
)

func newDesktopCommand() *cli.Command {
	return &cli.Command{
		Name:      "desktop",
		Usage:     "Open a graphical sandbox in your browser",
		ArgsUsage: "[<sandbox>]",
		Description: `Turns on the public URL for a sandbox running a desktop image, waits
for its desktop to finish starting, and prints a link you can open in
a browser to watch and control it.

The sandbox must already be running a desktop image. To create one:

    createos sandbox create --rootfs desktop:1

Anyone with the link can control the desktop, so treat it like a
password. It expires, and running this again issues a fresh link.`,
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "screen",
				Aliases: []string{"S"},
				Usage:   "Which screen to open",
				Value:   api.DefaultComputerScreen,
			},
			&cli.DurationFlag{
				Name:  "wait",
				Usage: "How long to wait for the desktop to start",
				Value: desktopReadyTimeout,
			},
		},
		Action: runDesktop,
	}
}

func runDesktop(c *cli.Context) error {
	client, ok := c.App.Metadata[api.SandboxClientKey].(*api.SandboxClient)
	if !ok {
		return fmt.Errorf("you're not signed in — run 'createos login' to get started")
	}

	ref := strings.TrimSpace(c.Args().First())
	var id string
	switch {
	case ref != "":
		resolved, err := resolveSandboxRef(c.Context, client, ref)
		if err != nil {
			return err
		}
		id = resolved
	case terminal.IsInteractive():
		picked, label, err := pickByStatus(c, client, "Pick a sandbox to open", api.SandboxStatusRunning)
		if err != nil {
			return err
		}
		if picked == "" {
			fmt.Println("Cancelled. Nothing changed.")
			return nil
		}
		id, ref = picked, label
	default:
		return fmt.Errorf("please provide a sandbox ID or name\n\n  To see your sandboxes, run:\n    createos sandbox list")
	}

	sb, err := client.GetSandbox(c.Context, id)
	if err != nil {
		return err
	}
	if err := ensureDesktopReady(c, client, sb); err != nil {
		return err
	}

	screen := c.String("screen")
	if !sb.IngressEnabled {
		if _, err := client.SetSandboxIngress(c.Context, id, true); err != nil {
			return fmt.Errorf("couldn't turn on the public URL for %s: %w", refLabel(ref, id), err)
		}
	}

	if err := waitForDesktop(c.Context, client, id, screen, c.Duration("wait")); err != nil {
		return err
	}

	conn, err := client.ComputerConnect(c.Context, id, screen)
	if err != nil {
		return err
	}
	if conn.URL == "" {
		return fmt.Errorf("no link came back for %s\n\n  The public URL has to be on before a link can be issued. Turn it on with:\n    createos sandbox edit %s --ingress on", refLabel(ref, id), id)
	}

	output.Render(c, conn, func() {
		pterm.Success.Printfln("Desktop ready on %s (%s)", refLabel(ref, id), screen)
		fmt.Printf("    %s\n", conn.URL)
		pterm.Println(pterm.Gray("  Anyone with this link can control the desktop."))
		if conn.ExpiresAt != "" {
			pterm.Println(pterm.Gray(fmt.Sprintf("  It expires at %s. Running this again issues a new one.", conn.ExpiresAt)))
		}
	})
	return nil
}

// ensureDesktopReady refuses early on a sandbox that cannot serve a desktop.
// Without this the first computer call fails with a far less obvious message
// than saying so up front.
func ensureDesktopReady(c *cli.Context, client *api.SandboxClient, sb *api.SandboxView) error {
	if sb.Status == api.SandboxStatusPaused {
		if _, err := client.ResumeSandbox(c.Context, sb.ID); err != nil {
			return err
		}
		spinner, _ := pterm.DefaultSpinner.Start("Waking the sandbox up…") //nolint:errcheck
		resumed, err := waitForStatus(c.Context, client, sb.ID, api.SandboxStatusRunning)
		if err != nil {
			spinner.Fail("It didn't wake up")
			return err
		}
		spinner.Success("Sandbox is awake")
		sb = resumed
	}
	if sb.Status != api.SandboxStatusRunning {
		return fmt.Errorf("that sandbox is %s, so it has no desktop to show yet\n\n  To check on it, run:\n    createos sandbox get %s", sb.Status, sb.ID)
	}
	rootfs := ""
	if sb.Rootfs != nil {
		rootfs = *sb.Rootfs
	}
	if !strings.Contains(strings.ToLower(rootfs), "desktop") {
		named := rootfs
		if named == "" {
			named = "an image without a desktop"
		}
		return fmt.Errorf("that sandbox runs %s, which has no desktop\n\n  Create one that does with:\n    createos sandbox create --rootfs desktop:1", named)
	}
	return nil
}

// waitForDesktop polls the screen route until the desktop answers. It stops
// early on an error that more waiting cannot fix — a missing desktop image
// answers 501 forever, and spinning on that just delays the real message.
func waitForDesktop(ctx context.Context, client *api.SandboxClient, id, screen string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	var spinner *pterm.SpinnerPrinter

	for {
		_, err := client.ComputerScreen(ctx, id, screen)
		if err == nil {
			if spinner != nil {
				spinner.Success("Desktop is up")
			}
			return nil
		}

		var computerErr *api.ComputerError
		if errors.As(err, &computerErr) && !computerErr.Retryable() {
			if spinner != nil {
				spinner.Fail("The desktop didn't start")
			}
			return err
		}
		if time.Now().After(deadline) {
			if spinner != nil {
				spinner.Fail("The desktop didn't start in time")
			}
			return fmt.Errorf("the desktop on %s didn't start within %s\n\n  To see whether it's still coming up, run:\n    createos sandbox exec %s 'pgrep -a Xvfb; pgrep -a websockify'", id, timeout, id)
		}
		if spinner == nil {
			spinner, _ = pterm.DefaultSpinner.Start("Waiting for the desktop to start…") //nolint:errcheck
		}

		select {
		case <-ctx.Done():
			if spinner != nil {
				spinner.Fail("Cancelled")
			}
			return ctx.Err()
		case <-time.After(desktopPollInterval):
		}
	}
}
