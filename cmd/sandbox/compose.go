package sandbox

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/urfave/cli/v2"

	"github.com/NodeOps-app/createos-cli/internal/api"
)

// The composed verbs (offload, matrix) share one box recipe. Keeping the
// flags in one place is what stops `offload --shape` and `matrix --shape`
// from drifting apart.
const (
	composeDefaultShape  = "s-1vcpu-1gb"
	composeDefaultRootfs = "devbox:1"
	// composeDefaultAutoPause is the backstop. If this CLI is killed
	// mid-run — a closed laptop, a dropped SSH session, a CI timeout —
	// nothing is left to destroy the boxes it created, and they bill
	// until someone notices. Auto-pause makes the sandbox park itself.
	composeDefaultAutoPause = 15 * time.Minute
	// composeWorkDir is where a staged tree lands inside the sandbox.
	composeWorkDir = "/work"
)

// composeFlags are the box-shape flags shared by offload and matrix.
func composeFlags() []cli.Flag {
	return []cli.Flag{
		&cli.StringFlag{Name: "shape", Value: composeDefaultShape, Usage: "Sandbox size (run 'createos sandbox shapes' to see options)"},
		&cli.StringFlag{Name: "rootfs", Value: composeDefaultRootfs, Usage: "Base image or template to boot from"},
		&cli.StringSliceFlag{Name: "env", Usage: "Environment variable for every command (repeatable): KEY=VALUE"},
		&cli.StringSliceFlag{Name: "egress", Usage: "Host the sandbox may reach (repeatable). Default: unrestricted"},
		&cli.StringSliceFlag{Name: "egress-preset", Usage: "Toolchain allowlist (repeatable): " + strings.Join(egressPresetNames(), ", ")},
		&cli.StringSliceFlag{Name: "exclude", Usage: "Path to keep out of the upload (repeatable). .gitignore is honoured already"},
		&cli.DurationFlag{Name: "auto-pause", Value: composeDefaultAutoPause, Usage: "Park an idle sandbox if this command dies before it can clean up. 0 disables"},
		&cli.DurationFlag{Name: "timeout", Usage: "Give up on a command after this long. Default: no limit"},
	}
}

// composeOptions is composeFlags after parsing and validation.
type composeOptions struct {
	Shape     string
	Rootfs    string
	Env       map[string]string
	Egress    []string
	Exclude   []string
	AutoPause time.Duration
	Timeout   time.Duration
}

func parseComposeOptions(c *cli.Context) (*composeOptions, error) {
	env, err := parseKeyValues(c.StringSlice("env"))
	if err != nil {
		return nil, err
	}
	egress, err := resolveEgress(c.StringSlice("egress-preset"), c.StringSlice("egress"))
	if err != nil {
		return nil, err
	}
	return &composeOptions{
		Shape:     c.String("shape"),
		Rootfs:    c.String("rootfs"),
		Env:       env,
		Egress:    egress,
		Exclude:   c.StringSlice("exclude"),
		AutoPause: c.Duration("auto-pause"),
		Timeout:   c.Duration("timeout"),
	}, nil
}

// checkMisplacedFlags turns a silent drop into a clear error.
//
// urfave/cli stops parsing flags at the first positional argument, so
// `matrix . --job 'x'` parses `.` and then treats --job as a plain
// argument: the job list comes back empty and nothing says why. This is a
// standing trap in this CLI — the same shape already bit `process run
// <box> --cwd` (commit 8c1f7ac) and `sandbox shapes -o json`.
//
// A user cannot be expected to know where the parser gave up, so any
// leftover argument that names one of this command's own flags is
// reported with the corrected command line.
func checkMisplacedFlags(c *cli.Context, leftovers []string) error {
	known := make(map[string]bool)
	for _, f := range c.Command.Flags {
		for _, n := range f.Names() {
			known["--"+n] = true
			known["-"+n] = true
		}
	}
	for _, arg := range leftovers {
		name, _, _ := strings.Cut(arg, "=")
		if known[name] {
			return fmt.Errorf(
				"%s was written after the directory, so it was ignored\n\n  Flags must come before the directory:\n    createos sandbox %s %s <directory>",
				name, c.Command.Name, name)
		}
	}
	return nil
}

// parseKeyValues turns repeated KEY=VALUE flags into a map.
func parseKeyValues(pairs []string) (map[string]string, error) {
	if len(pairs) == 0 {
		return nil, nil
	}
	out := make(map[string]string, len(pairs))
	for _, p := range pairs {
		k, v, ok := strings.Cut(p, "=")
		if !ok || k == "" {
			return nil, fmt.Errorf("--env %q is not KEY=VALUE", p)
		}
		out[k] = v
	}
	return out, nil
}

// createComposeBox boots one sandbox to the shared recipe and waits for it
// to run.
func createComposeBox(ctx context.Context, client *api.SandboxClient, opts *composeOptions) (*api.SandboxView, error) {
	// No name: these boxes are machinery with a lifetime of one command,
	// and a generated name is easier to tell apart in `sandbox ls` than a
	// dozen boxes all called the same thing.
	req := api.SandboxCreateReq{
		Shape:  opts.Shape,
		Rootfs: opts.Rootfs,
		Egress: opts.Egress,
		Envs:   opts.Env,
	}
	if opts.AutoPause > 0 {
		secs := int(opts.AutoPause.Seconds())
		req.AutoPauseAfterSeconds = &secs
	}
	created, err := client.CreateSandbox(ctx, req)
	if err != nil {
		return nil, err
	}
	// Past this point the sandbox exists and is billable. Every way out
	// that is not "running" has to destroy it, or a readiness timeout
	// leaves a machine nobody knows about — offload and matrix only see
	// the error, never the id.
	sb, err := waitForStatus(ctx, client, created.ID, "running")
	if err != nil {
		return nil, cleanupAfterCreate(ctx, client, created.ID, err)
	}
	if sb.Status != "running" {
		return nil, cleanupAfterCreate(ctx, client, sb.ID,
			fmt.Errorf("sandbox %s came up %s, not running", sb.ID, sb.Status))
	}
	return sb, nil
}

// cleanupAfterCreate destroys a sandbox that never became usable and folds
// the outcome into the error the caller sees. The id is always named: if
// the teardown itself fails, the user needs it to clean up by hand.
func cleanupAfterCreate(ctx context.Context, client *api.SandboxClient, id string, cause error) error {
	tearCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()
	if err := client.DestroySandbox(tearCtx, id); err != nil {
		return fmt.Errorf("%w\n\n  Sandbox %s was created and could not be destroyed (%w).\n  It is still billable. Remove it with:\n    createos sandbox rm --force %s",
			cause, id, err, id)
	}
	return fmt.Errorf("%w\n\n  Sandbox %s was destroyed", cause, id)
}

// managedResult is one command's outcome.
type managedResult struct {
	ExitCode int
	Signal   string
	Duration time.Duration
}

// composeStreamRetries bounds how many times runManaged reconnects to a
// live process after the output stream drops.
const composeStreamRetries = 5

// runManaged runs cmd as a managed process and streams its output to out.
//
// A managed process is the point. `sandbox exec` ties the command's life
// to the HTTP stream, so a connection that drops on a long quiet build —
// a laptop sleeping, a proxy idle-timeout — kills the remote command. A
// managed process keeps running on the box and keeps replayable output, so
// this function reconnects from the last sequence it saw and picks the
// output back up. That reconnect is the whole reason the composed verbs
// survive builds that print nothing for ten minutes.
func runManaged(
	ctx context.Context,
	client *api.SandboxClient,
	sandboxID, cmd, cwd string,
	env map[string]string,
	out io.Writer,
) (*managedResult, error) {
	start := time.Now()
	proc, err := client.CreateProcess(ctx, sandboxID, api.ProcessCreateRequest{
		Cmd:  "bash",
		Args: []string{"-lc", cmd},
		Cwd:  cwd,
		Env:  env,
	})
	if err != nil {
		return nil, fmt.Errorf("start command in %s: %w", sandboxID, err)
	}

	var exitCode *int
	var signal string
	after := int64(0)

	for attempt := 0; ; attempt++ {
		streamErr := client.ConnectProcess(ctx, sandboxID, proc.ProcessID, after, func(ev api.ProcessOutputEvent) {
			switch ev.Type {
			case "data":
				if ev.Seq > after {
					after = ev.Seq
				}
				if raw, decErr := base64.StdEncoding.DecodeString(ev.DataBase64); decErr == nil {
					_, _ = out.Write(raw) //nolint:errcheck // a failed log write must not kill the job
				}
			case "exit":
				exitCode = ev.ExitCode
				signal = ev.Signal
			}
		})
		if exitCode != nil {
			break
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if streamErr == nil || errors.Is(streamErr, context.Canceled) {
			// Stream ended cleanly but no exit frame arrived. Ask the
			// server directly rather than guessing.
			done, waitErr := client.WaitProcess(ctx, sandboxID, proc.ProcessID, true, int64(pollTimeout/time.Millisecond))
			if waitErr != nil {
				return nil, waitErr
			}
			if done.ExitCode != nil {
				exitCode = done.ExitCode
				signal = done.Signal
				break
			}
		}
		if attempt >= composeStreamRetries {
			return nil, fmt.Errorf("lost the output stream for %s in %s after %d reconnects: %w",
				proc.ProcessID, sandboxID, attempt, streamErr)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(time.Second):
		}
	}

	return &managedResult{ExitCode: *exitCode, Signal: signal, Duration: time.Since(start)}, nil
}

// destroyQuiet tears a sandbox down and reports failures without stopping
// the caller. A composed verb is usually already unwinding when it calls
// this, and a teardown error must not mask the real one — but it must not
// be swallowed either, because the sandbox is still billable.
func destroyQuiet(ctx context.Context, client *api.SandboxClient, id string, warn func(string)) {
	// The caller's context may already be cancelled (Ctrl-C, timeout).
	// Teardown still has to happen, so give it a context of its own.
	tearCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()
	if err := client.DestroySandbox(tearCtx, id); err != nil && warn != nil {
		warn(fmt.Sprintf("could not destroy %s: %v — remove it with: createos sandbox rm --force %s", id, err, id))
	}
}
