package sandbox

import (
	"fmt"
	"strings"

	"github.com/pterm/pterm"
	"github.com/urfave/cli/v2"

	"github.com/NodeOps-app/createos-cli/internal/api"
	"github.com/NodeOps-app/createos-cli/internal/terminal"
)

func newResumeCommand() *cli.Command {
	return &cli.Command{
		Name:      "resume",
		Usage:     "Bring a paused sandbox back to life",
		ArgsUsage: "[<sandbox>]",
		Description: `Resume restores a paused sandbox — possibly on a different machine.
The sandbox keeps its name, ID, disks, networks, and stored env vars.

Run with no argument on a terminal to pick from your paused sandboxes.`,
		Action: runResume,
	}
}

func runResume(c *cli.Context) error {
	client, ok := c.App.Metadata[api.SandboxClientKey].(*api.SandboxClient)
	if !ok {
		return fmt.Errorf("you're not signed in — run 'createos login' to get started")
	}
	ref := strings.TrimSpace(c.Args().First())
	if ref == "" {
		if !terminal.IsInteractive() {
			return fmt.Errorf("please provide a sandbox ID or name\n\n  To see your paused sandboxes, run:\n    createos sandbox list --status paused")
		}
		id, label, err := pickByStatus(c, client, "Pick a sandbox to resume", "paused")
		if err != nil {
			return err
		}
		if id == "" {
			fmt.Println("Cancelled. Nothing changed.")
			return nil
		}
		return runResumeByID(c, client, label, id)
	}
	id, err := resolveSandboxRef(c.Context, client, ref)
	if err != nil {
		return err
	}
	return runResumeByID(c, client, ref, id)
}

func runResumeByID(c *cli.Context, client *api.SandboxClient, ref, id string) error {
	if _, err := client.ResumeSandbox(c.Context, id); err != nil {
		return err
	}
	spinner, _ := pterm.DefaultSpinner.Start(fmt.Sprintf("Resuming %s…", refLabel(ref, id))) //nolint:errcheck
	sb, err := waitForStatus(c.Context, client, id, "running")
	if err != nil {
		spinner.Fail("Resume did not complete")
		return err
	}
	if sb.Status != "running" {
		spinner.Fail(fmt.Sprintf("Resume ended in %q", sb.Status))
		return fmt.Errorf("sandbox %s is %s — see `createos sandbox get %s` for details", refLabel(ref, id), sb.Status, id)
	}
	spinner.Success(fmt.Sprintf("Resumed %s", refLabel(ref, id)))
	return nil
}
