package sandbox

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/pterm/pterm"
	"github.com/urfave/cli/v2"

	"github.com/NodeOps-app/createos-cli/internal/api"
	"github.com/NodeOps-app/createos-cli/internal/dclock"
	"github.com/NodeOps-app/createos-cli/internal/terminal"
)

// newDCDownCommand wires `createos sandbox dc down`.
//
// Default: stop compose (best-effort) + destroy the sandbox + remove
// the lockfile. The destroy makes the sandbox unrecoverable, so we
// confirm on a TTY unless --yes is passed.
//
// --keep skips the destroy: we run `docker compose down` inside the VM
// and leave the sandbox running. Useful when you want to teardown the
// stack but reuse the same VM (saves the next 'up' from waiting on a
// fresh sandbox boot + image pull).
//
// --volumes passes `-v` to `docker compose down` so named volumes are
// also removed. Without it, postgres / redis / etc. data persists in
// the VM and is picked up by the next 'up'.
func newDCDownCommand() *cli.Command {
	return &cli.Command{
		Name:  "down",
		Usage: "Tear down the compose stack (and the sandbox unless --keep)",
		Description: `By default destroys the sandbox entirely — the fastest path back to
zero cost. Pass --keep to only stop compose and leave the sandbox
running.

Examples:
  createos sb dc down
  createos sb dc down --keep
  createos sb dc down -v          # also remove named docker volumes
  createos sb dc down --yes       # skip the confirmation prompt`,
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "file",
				Aliases: []string{"f"},
				Usage:   "Path to docker-compose.yml (default: ./docker-compose.yml)",
				Value:   "docker-compose.yml",
			},
			&cli.BoolFlag{
				Name:  "keep",
				Usage: "Stop compose only; keep the sandbox running",
			},
			&cli.BoolFlag{
				Name:    "volumes",
				Aliases: []string{"v"},
				Usage:   "Pass -v to 'docker compose down' (remove named volumes)",
			},
			&cli.BoolFlag{
				Name:    "yes",
				Aliases: []string{"y"},
				Usage:   "Skip the confirmation prompt (required in non-interactive mode for destroy)",
			},
		},
		Action: runDCDown,
	}
}

func runDCDown(c *cli.Context) error {
	client, ok := c.App.Metadata[api.SandboxClientKey].(*api.SandboxClient)
	if !ok {
		return fmt.Errorf("you're not signed in — run 'createos login' to get started")
	}
	projectDir, lock, err := loadDCProject(c.String("file"))
	if err != nil {
		return err
	}

	// Confirm on a TTY when about to destroy. --keep doesn't need this:
	// stopping compose is recoverable.
	if !c.Bool("keep") {
		if !c.Bool("yes") {
			if !terminal.IsInteractive() {
				return fmt.Errorf("non-interactive mode: pass --yes to confirm destroying sandbox %s", lock.SandboxID)
			}
			ok, perr := pterm.DefaultInteractiveConfirm.
				WithDefaultText(fmt.Sprintf("Destroy sandbox %s? This deletes its disk and snapshot.", lock.SandboxID)).
				WithDefaultValue(false).
				Show()
			if perr != nil {
				return fmt.Errorf("could not read confirmation: %w", perr)
			}
			if !ok {
				fmt.Println("Cancelled. Nothing was destroyed.")
				return nil
			}
		}
	}

	// 1. Best-effort `docker compose down` so named volumes survive a
	//    'down --keep' cycle cleanly. Skipped if the sandbox is no
	//    longer running — there's nothing to gracefully stop.
	if err := composeDown(c.Context, client, lock, c.Bool("volumes")); err != nil {
		pterm.Warning.Println("compose down failed (continuing): " + err.Error())
	} else {
		pterm.Success.Println("Compose stack stopped.")
	}

	// 1b. Terminate any active mutagen sync session. Best-effort: if
	//     mutagen isn't installed (e.g. someone deleted ~/.createos),
	//     just drop the lockfile entry and move on.
	if lock.Sync != nil && lock.Sync.SessionName != "" {
		if err := terminateDCSync(c.Context, lock); err != nil {
			pterm.Warning.Println("terminate sync session failed (continuing): " + err.Error())
		} else {
			pterm.Success.Println("Sync session terminated.")
		}
		lock.Sync = nil
	}

	// 2. --keep stops here.
	if c.Bool("keep") {
		// Clear ports from the lockfile — they're no longer bound, and
		// a stale port list would confuse 'dc ps'. Sandbox id stays.
		lock.Ports = nil
		if err := lock.Save(projectDir); err != nil {
			return fmt.Errorf("update lockfile: %w", err)
		}
		pterm.Info.Println("Sandbox " + lock.SandboxID + " left running.")
		return nil
	}

	// 3. Destroy the sandbox.
	if err := client.DestroySandbox(c.Context, lock.SandboxID); err != nil {
		// Treat "not found" as already-destroyed — clean up the lockfile
		// either way so the user isn't stuck.
		pterm.Warning.Println("destroy failed (will remove lockfile anyway): " + err.Error())
	} else {
		pterm.Success.Println("Sandbox " + lock.SandboxID + " destroyed.")
	}

	// 4. Remove the lockfile so the next 'up' creates a fresh sandbox.
	if err := dclock.Remove(projectDir); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove lockfile: %w", err)
	}
	return nil
}

// composeDown runs `docker compose down [-v]` inside the sandbox via
// the exec stream so the user sees the per-container stop messages
// in real time. Soft-fails when the sandbox isn't running so 'dc down'
// still completes its destroy step.
func composeDown(ctx context.Context, client *api.SandboxClient, lock *dclock.Lock, removeVolumes bool) error {
	args := []string{
		"compose",
		"-p", lock.ProjectName,
		"-f", lock.ComposeFile,
		"down",
	}
	if removeVolumes {
		args = append(args, "-v")
	}
	exit, err := client.ExecSandboxStream(ctx, lock.SandboxID, api.SandboxExecReq{
		Cmd:  "docker",
		Args: args,
	}, func(ev api.SandboxExecStreamEvent) {
		switch {
		case ev.Stdout != "":
			_, _ = io.WriteString(os.Stdout, ev.Stdout) //nolint:errcheck
		case ev.Stderr != "":
			_, _ = io.WriteString(os.Stderr, ev.Stderr) //nolint:errcheck
		case ev.Error != "":
			pterm.Error.Println(ev.Error)
		}
	})
	if err != nil {
		return err
	}
	if exit != 0 {
		return fmt.Errorf("docker compose down exited %d", exit)
	}
	return nil
}
