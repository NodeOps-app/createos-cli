package sandbox

import (
	"fmt"
	"strings"

	"github.com/pterm/pterm"
	"github.com/urfave/cli/v2"

	"github.com/NodeOps-app/createos-cli/internal/api"
	"github.com/NodeOps-app/createos-cli/internal/terminal"
)

func newPauseCommand() *cli.Command {
	return &cli.Command{
		Name:      "pause",
		Usage:     "Snapshot a running sandbox so you can resume it later",
		ArgsUsage: "[<sandbox>]",
		Description: `Pause snapshots the sandbox to durable storage and tears down
the live VM. Resume restores it (possibly on a different machine).
The sandbox keeps its name, ID, disks, networks, and stored env vars.

Run with no argument on a terminal to pick from your running sandboxes.`,
		Action: runPause,
	}
}

func runPause(c *cli.Context) error {
	client, ok := c.App.Metadata[api.SandboxClientKey].(*api.SandboxClient)
	if !ok {
		return fmt.Errorf("you're not signed in — run 'createos login' to get started")
	}
	ref := strings.TrimSpace(c.Args().First())
	if ref == "" {
		if !terminal.IsInteractive() {
			return fmt.Errorf("please provide a sandbox ID or name\n\n  To see your running sandboxes, run:\n    createos sandbox list")
		}
		id, label, err := pickByStatus(c, client, "Pick a sandbox to pause", "running")
		if err != nil {
			return err
		}
		if id == "" {
			fmt.Println("Cancelled. Nothing changed.")
			return nil
		}
		return runPauseByID(c, client, label, id)
	}
	id, err := resolveSandboxRef(c.Context, client, ref)
	if err != nil {
		return err
	}
	return runPauseByID(c, client, ref, id)
}

func runPauseByID(c *cli.Context, client *api.SandboxClient, ref, id string) error {
	if _, err := client.PauseSandbox(c.Context, id); err != nil {
		return err
	}
	spinner, _ := pterm.DefaultSpinner.Start(fmt.Sprintf("Pausing %s…", refLabel(ref, id)))
	sb, err := waitForStatus(c.Context, client, id, "paused")
	if err != nil {
		spinner.Fail("Pause did not complete")
		return err
	}
	if sb.Status != "paused" {
		spinner.Fail(fmt.Sprintf("Pause ended in %q", sb.Status))
		return fmt.Errorf("sandbox %s is %s — see `createos sandbox get %s` for details", refLabel(ref, id), sb.Status, id)
	}
	spinner.Success(fmt.Sprintf("Paused %s", refLabel(ref, id)))
	return nil
}
