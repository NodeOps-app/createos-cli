package sandbox

import (
	"context"
	"fmt"
	"strconv"
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

Pass --count to take several clones of one prepared sandbox — a golden
box with the toolchain and dependencies already installed, cloned once
per test job or per user. Each clone is independent.

Run with no argument on a terminal to pick from your paused sandboxes.

Examples:
  # One clone, resumed and ready
  createos sandbox fork my-golden-box

  # Ten independent clones, left paused so you resume them when needed
  createos sandbox fork my-golden-box --count 10 --paused

A forked sandbox comes up WITHOUT the S3 disks the source had mounted.
Re-attach them after the fork resumes.`,
		Flags: []cli.Flag{
			&cli.BoolFlag{
				Name:  "paused",
				Usage: "Leave the new sandbox paused instead of auto-resuming",
			},
			&cli.IntFlag{
				Name:  "count",
				Value: 1,
				Usage: "Number of clones to take from the same snapshot",
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

	if count := forkCountFlag(c); count < 1 {
		return fmt.Errorf("--count must be at least 1 (got %d)", count)
	}

	ref := strings.TrimSpace(c.Args().First())
	if ref == "" {
		if !terminal.IsInteractive() {
			return fmt.Errorf("please provide a sandbox ID or name to fork from\n\n  To see your paused sandboxes, run:\n    createos sandbox list --status paused")
		}
		id, label, err := pickByStatus(c, client, "Pick a sandbox to fork", "paused")
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
	count := forkCountFlag(c)

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
		forks, err := forkN(c.Context, client, srcID, req, count, nil)
		if err != nil {
			return err
		}
		if count == 1 {
			output.Render(c, forks[0], func() {})
			return nil
		}
		output.Render(c, forks, func() {})
		return nil
	}

	warnForkDropsDisks(c.Context, client, srcID)

	label := refLabel(ref, srcID)
	noun := "Forking %s…"
	if count > 1 {
		noun = fmt.Sprintf("Forking %%s into %d clones…", count)
	}
	spinner, _ := pterm.DefaultSpinner.Start(fmt.Sprintf(noun, label)) //nolint:errcheck
	forks, err := forkN(c.Context, client, srcID, req, count, func(done, total int) {
		if total > 1 {
			spinner.UpdateText(fmt.Sprintf("Forking %s — %d/%d ready…", label, done, total))
		}
	})
	if err != nil {
		spinner.Fail("Fork failed")
		return err
	}

	spinner.Success(fmt.Sprintf("Forked %s into %d sandbox(es)", label, len(forks)))
	for _, sb := range forks {
		name := ""
		if sb.Name != nil {
			name = *sb.Name
		}
		fmt.Printf("    %s\n", refLabel(name, sb.ID))
		if sb.IP != nil && *sb.IP != "" {
			fmt.Printf("      IP:  %s\n", *sb.IP)
		}
		if sb.IngressURLTemplate != "" {
			fmt.Printf("      URL: %s\n", sb.IngressURLTemplate)
		}
	}
	return nil
}

// ensureForkable brings srcID to `paused`, which is the only state fork
// accepts. Pause is asynchronous: it answers while the sandbox is still
// `pausing`, so a fork issued right after a pause used to be rejected with
// "sandbox is running, expected paused or error". Waiting here is what
// makes pause-then-fork safe for callers, matrix included.
func ensureForkable(ctx context.Context, client *api.SandboxClient, srcID string) error {
	sb, err := client.GetSandbox(ctx, srcID)
	if err != nil {
		return err
	}
	switch sb.Status {
	case "paused":
		return nil
	case "pausing":
		// Already on its way down; waiting is not a decision we are making
		// on the user's behalf.
		return waitUntilPaused(ctx, client, srcID)
	case "running":
		// Deliberately NOT pausing here. Pausing a running sandbox stops
		// whatever it is serving, and `fork` must never do that as a side
		// effect — the source could be a live dev server or a demo someone
		// is using. Callers that own the sandbox pause it themselves.
		return fmt.Errorf(
			"sandbox %s is running, and fork needs a paused snapshot\n\n  Pausing stops whatever it is serving, so fork will not do it for you.\n  Pause it yourself, then fork:\n    createos sandbox pause %s\n    createos sandbox fork %s",
			srcID, srcID, srcID)
	default:
		return fmt.Errorf("sandbox %s is %s — fork needs it paused\n\n  Run:\n    createos sandbox pause %s", srcID, sb.Status, srcID)
	}
}

// pauseForFork pauses a sandbox the caller owns and waits for the snapshot
// to settle. Only matrix uses this, on the golden box it created itself —
// which is the one case where pausing is not a surprise to anyone.
func pauseForFork(ctx context.Context, client *api.SandboxClient, srcID string) error {
	sb, err := client.GetSandbox(ctx, srcID)
	if err != nil {
		return err
	}
	switch sb.Status {
	case "paused":
		return nil
	case "pausing":
	case "running":
		if _, pauseErr := client.PauseSandbox(ctx, srcID); pauseErr != nil {
			return fmt.Errorf("pause %s before forking: %w", srcID, pauseErr)
		}
	default:
		return fmt.Errorf("sandbox %s is %s — it cannot be paused for forking", srcID, sb.Status)
	}
	return waitUntilPaused(ctx, client, srcID)
}

// waitUntilPaused blocks until the snapshot is on disk. Pause is async: it
// answers while the sandbox is still `pausing`, and a fork issued in that
// window is rejected with "sandbox is running, expected paused or error".
func waitUntilPaused(ctx context.Context, client *api.SandboxClient, srcID string) error {
	final, err := waitForStatus(ctx, client, srcID, "paused")
	if err != nil {
		return err
	}
	if final.Status != "paused" {
		return fmt.Errorf("sandbox %s ended in %q while pausing — see `createos sandbox get %s`", srcID, final.Status, srcID)
	}
	return nil
}

// forkN takes count clones of one paused snapshot and waits for each to
// reach its target state. onProgress, when non-nil, is called after every
// clone settles.
//
// Clones run one at a time on purpose. Fork is a server-side object copy
// measured at about a second, so the wall-clock saving from parallelism is
// small, while a partial failure halfway through a parallel batch leaves
// an unknown number of billable sandboxes behind. Sequential means the
// error names exactly how many exist.
func forkN(
	ctx context.Context,
	client *api.SandboxClient,
	srcID string,
	req api.SandboxForkReq,
	count int,
	onProgress func(done, total int),
) ([]*api.SandboxView, error) {
	if err := ensureForkable(ctx, client, srcID); err != nil {
		return nil, err
	}
	target := "running"
	if req.StartPaused {
		target = "paused"
	}

	// created tracks every id the server handed back, settled or not.
	// forks holds only the ones that reached `target`. The split matters:
	// a fork whose status poll times out still exists and still bills, and
	// reporting only the settled ones hides it from the caller — which,
	// for matrix, is the difference between a cleaned-up failure and an
	// orphaned running sandbox.
	forks := make([]*api.SandboxView, 0, count)
	created := make([]string, 0, count)
	for i := 0; i < count; i++ {
		view, err := client.ForkSandbox(ctx, srcID, req)
		if err != nil {
			return forks, forkPartialError(err, created, i, count)
		}
		created = append(created, view.ID)

		sb, err := waitForStatus(ctx, client, view.ID, target)
		if err != nil {
			return forks, forkPartialError(err, created, i, count)
		}
		if sb.Status != target {
			return forks, forkPartialError(
				fmt.Errorf("sandbox %s is %s, expected %s", sb.ID, sb.Status, target), created, i, count)
		}
		forks = append(forks, sb)
		if onProgress != nil {
			onProgress(len(forks), count)
		}
	}
	return forks, nil
}

// forkPartialError names every clone the server created, so a failed batch
// does not leave billable sandboxes the caller cannot find. It carries the
// ids as a forkLeak so a caller that can clean up — matrix — does not have
// to parse them back out of the message.
func forkPartialError(err error, created []string, attempt, total int) error {
	if len(created) == 0 {
		return err
	}
	return &forkLeak{
		IDs: created,
		err: fmt.Errorf("fork %d of %d failed: %w\n\n  %d clone(s) exist and are still billable:\n    %s\n\n  Remove them with:\n    createos sandbox rm --force %s",
			attempt+1, total, err, len(created), strings.Join(created, "\n    "), strings.Join(created, " ")),
	}
}

// forkLeak reports the sandboxes a failed fork batch left behind.
type forkLeak struct {
	IDs []string
	err error
}

func (e *forkLeak) Error() string { return e.err.Error() }
func (e *forkLeak) Unwrap() error { return e.err }

// forkCountFlag reads --count the normal way, and falls back to a raw scan
// of os.Args when that comes back unset.
//
// Go's stdlib flag package, which urfave/cli sits on, stops parsing at the
// first non-flag argument. `fork <sandbox> --count 2` writes the sandbox
// first — the natural order — so `--count` is never parsed as a flag at
// all: it lands unread in c.Args(), and c.Int("count") silently returns
// the flag's default (1). No error, just the wrong count. This is the same
// shape of bug fixed for `process run <box> --cwd` (commit 8c1f7ac); the
// fallback below reuses that fix's own raw-argv scanner.
func forkCountFlag(c *cli.Context) int {
	if c.IsSet("count") {
		return c.Int("count")
	}
	raw := rawProcessFlagValue("fork", "count")
	if raw == "" {
		return c.Int("count")
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return c.Int("count")
	}
	return n
}
