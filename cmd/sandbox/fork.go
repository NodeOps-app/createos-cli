package sandbox

import (
	"fmt"
	"strings"

	"github.com/pterm/pterm"
	"github.com/urfave/cli/v2"

	"github.com/NodeOps-app/createos-cli/internal/api"
	"github.com/NodeOps-app/createos-cli/internal/output"
	"github.com/NodeOps-app/createos-cli/internal/terminal"
)

func newForkCommand() *cli.Command {
	return &cli.Command{
		Name:      "fork",
		Usage:     "Clone a paused sandbox into a brand-new one",
		ArgsUsage: "[<sandbox>]",
		Description: `Fork copies a paused sandbox's snapshot into a new sandbox ID. By
default the fork auto-resumes; pass --paused to keep it paused so you
can fork again or attach things first.

Run with no argument on a terminal to pick from your paused sandboxes.`,
		Flags: []cli.Flag{
			&cli.BoolFlag{
				Name:  "paused",
				Usage: "Leave the new sandbox paused instead of auto-resuming",
			},
			&cli.StringSliceFlag{
				Name:  "ssh-key",
				Usage: "Override SSH public-key file for the fork (repeatable)",
			},
			&cli.StringSliceFlag{
				Name:  "egress",
				Usage: "Override egress allowlist for the fork (repeatable)",
			},
		},
		Action: runFork,
	}
}

func runFork(c *cli.Context) error {
	client, ok := c.App.Metadata[api.SandboxClientKey].(*api.SandboxClient)
	if !ok {
		return fmt.Errorf("you're not signed in — run 'createos login' to get started")
	}

	ref := strings.TrimSpace(c.Args().First())
	if ref == "" {
		if !terminal.IsInteractive() {
			return fmt.Errorf("please provide a sandbox ID or name to fork from\n\n  To see your paused sandboxes, run:\n    createos sandbox list --status paused")
		}
		id, label, err := pickByStatus(c, client, "Pick a sandbox to fork", api.SandboxStatusPaused)
		if err != nil {
			return err
		}
		if id == "" {
			fmt.Println("Cancelled. Nothing changed.")
			return nil
		}
		return runForkByID(c, client, label, id)
	}
	id, err := resolveSandboxRef(c.Context, client, ref)
	if err != nil {
		return err
	}
	return runForkByID(c, client, ref, id)
}

func runForkByID(c *cli.Context, client *api.SandboxClient, ref, srcID string) error {
	req := api.SandboxForkReq{
		StartPaused: c.Bool("paused"),
	}
	if keys, err := readSSHPubkeys(c.StringSlice("ssh-key")); err != nil {
		return err
	} else if len(keys) > 0 {
		req.SSHPubkeys = keys
	}
	if egress := c.StringSlice("egress"); len(egress) > 0 {
		req.Egress = egress
	}

	if output.IsJSON(c) {
		view, err := client.ForkSandbox(c.Context, srcID, req)
		if err != nil {
			return err
		}
		target := api.SandboxStatusRunning
		if req.StartPaused {
			target = api.SandboxStatusPaused
		}
		sb, err := waitForStatus(c.Context, client, view.ID, target)
		if err != nil {
			return err
		}
		output.Render(c, sb, func() {})
		return nil
	}

	spinner, _ := pterm.DefaultSpinner.Start(fmt.Sprintf("Forking %s…", refLabel(ref, srcID))) //nolint:errcheck
	view, err := client.ForkSandbox(c.Context, srcID, req)
	if err != nil {
		spinner.Fail("Fork failed")
		return err
	}

	target := api.SandboxStatusRunning
	if req.StartPaused {
		target = api.SandboxStatusPaused
	}
	sb, err := waitForStatus(c.Context, client, view.ID, target)
	if err != nil {
		spinner.Fail("Fork did not finish")
		return err
	}
	if sb.Status != target {
		spinner.Fail(fmt.Sprintf("Fork ended in %q", sb.Status))
		return fmt.Errorf("sandbox %s is %s — see `createos sandbox get %s` for details", sb.ID, sb.Status, sb.ID)
	}

	name := ""
	if sb.Name != nil {
		name = *sb.Name
	}
	spinner.Success(fmt.Sprintf("Forked into %s", refLabel(name, sb.ID)))
	if sb.IP != nil && *sb.IP != "" {
		fmt.Printf("    IP: %s\n", *sb.IP)
	}
	if sb.IngressURLTemplate != "" {
		fmt.Printf("    URL: %s\n", sb.IngressURLTemplate)
	}
	return nil
}
