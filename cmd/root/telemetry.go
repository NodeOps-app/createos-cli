// Package root — telemetry wiring helpers (Phase 2).
package root

import (
	"context"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"github.com/urfave/cli/v2"

	"github.com/NodeOps-app/createos-cli/internal/config"
	"github.com/NodeOps-app/createos-cli/internal/telemetry"
)

// helpEmittedThisProcess is a process-wide guard so the HelpPrinter override
// (Phase 3) and the Action wrapper don't double-emit command_invoked when both
// fire (e.g. `createos projects --help`).
var helpEmittedThisProcess atomic.Bool

// currentAppPtr holds a process-wide pointer to the active *cli.App so the
// HelpPrinter override (which has no cli.Context access) can stash invoked
// props into App.Metadata for the finalizer to pair with.
var currentAppPtr atomic.Pointer[cli.App]

// CurrentApp returns the active app pointer set by NewApp, or nil if not set.
func CurrentApp() *cli.App { return currentAppPtr.Load() }

// valueFlags lists global flags that take a value. Must be kept in sync with
// NewApp's Flags slice. Bools (--debug, -d) take no value, so omitted.
var valueFlags = map[string]bool{
	"--api-url": true,
	"--output":  true,
	"-o":        true,
}

// buildInvokedProps collects properties for a command_invoked event from the
// active cli.Context.
func buildInvokedProps(c *cli.Context) map[string]any {
	props := map[string]any{
		"command":   commandPath(c),
		"flags":     telemetry.FlagsFromContext(c),
		"arg_count": c.Args().Len(),
	}
	if pid := resolveProjectID(c); pid != "" {
		props["project_id"] = pid
	}
	return props
}

// commandPath returns the space-joined command name including all parent
// subcommand names (e.g. "projects list"). urfave/cli/v2 v2.27.7's
// c.Command.FullName() returns only the leaf name, so we walk c.Lineage()
// from root → leaf, skipping the synthesized root command whose Name
// equals the App.Name.
func commandPath(c *cli.Context) string {
	if c == nil || c.Command == nil {
		return ""
	}
	lineage := c.Lineage()
	rootName := ""
	if c.App != nil {
		rootName = c.App.Name
	}
	parts := make([]string, 0, len(lineage))
	for i := len(lineage) - 1; i >= 0; i-- {
		ctx := lineage[i]
		if ctx.Command == nil || ctx.Command.Name == "" {
			continue
		}
		// Skip the synthesized root command unless it is the only entry
		// (i.e. this IS the home-screen Action and "createos" is the name).
		if ctx.Command.Name == rootName && len(lineage) > 1 {
			continue
		}
		parts = append(parts, ctx.Command.Name)
	}
	if len(parts) == 0 {
		return c.Command.Name
	}
	return strings.Join(parts, " ")
}

// resolveProjectID picks (in order): --project flag, --project-id flag, then
// the linked .createos.json's ProjectID. Empty string when none resolve.
func resolveProjectID(c *cli.Context) string {
	if v := c.String("project"); v != "" {
		return v
	}
	if v := c.String("project-id"); v != "" {
		return v
	}
	if cfg, err := config.FindProjectConfig(); err == nil && cfg != nil {
		return cfg.ProjectID
	}
	return ""
}

// wrapActions wraps every command's Action so we capture command_invoked and
// stash invoked props into App.Metadata for the finalizer to pair with.
func wrapActions(cmds []*cli.Command) {
	for _, cmd := range cmds {
		if original := cmd.Action; original != nil {
			orig := original
			cmd.Action = func(c *cli.Context) error {
				props := buildInvokedProps(c)
				c.App.Metadata["telemetry_invoked_props"] = props
				if !c.Bool("help") {
					if telemetry.Default != nil {
						telemetry.Default.Capture("command_invoked", props)
					}
				}
				return orig(c)
			}
		}
		wrapActions(cmd.Subcommands)
	}
}

// wrapAppAction wraps the App-level Action (the home screen).
func wrapAppAction(app *cli.App) {
	if app.Action == nil {
		return
	}
	original := app.Action
	app.Action = func(c *cli.Context) error {
		props := buildInvokedProps(c)
		c.App.Metadata["telemetry_invoked_props"] = props
		if !c.Bool("help") {
			if telemetry.Default != nil {
				telemetry.Default.Capture("command_invoked", props)
			}
		}
		return original(c)
	}
}

// coarseCommandFromArgs extracts the first positional token from os.Args[1:],
// skipping flag tokens AND their values. Used by the finalizer ONLY when
// App.Before did not run (cli/v2 framework rejected args before Before fired).
func coarseCommandFromArgs(args []string) string {
	if len(args) < 2 {
		return ""
	}
	skipNext := false
	for _, a := range args[1:] {
		if skipNext {
			skipNext = false
			continue
		}
		if a == "" {
			continue
		}
		if strings.HasPrefix(a, "-") {
			if eq := strings.IndexByte(a, '='); eq >= 0 {
				// --flag=value form — value consumed inline, no skip needed.
				_ = a[:eq]
			} else if valueFlags[a] {
				skipNext = true
			}
			continue
		}
		return a
	}
	return ""
}

// finalizeTelemetry runs after app.Run returns. It emits command_completed or
// command_failed (paired with the command_invoked from wrapActions or the
// HelpPrinter override), then bounds-flushes the posthog client.
//
// Architecture: we deliberately do NOT use App.After — it cannot see the
// Action's return error and fires on Before-failure paths where it would
// mislabel errors as success.
func finalizeTelemetry(app *cli.App, err error) {
	client := telemetry.Default
	if client == nil {
		return
	}

	props, ok := app.Metadata["telemetry_invoked_props"].(map[string]any)
	if !ok || props == nil {
		// Action wrapper never ran. Build a coarse fallback. Prefer the value
		// stashed by App.Before (cli/v2 has already paired flags and values
		// there) over re-parsing os.Args.
		cmd, _ := app.Metadata["telemetry_arg_first"].(string)
		if cmd == "" {
			cmd = coarseCommandFromArgs(os.Args)
		}
		props = map[string]any{
			"command":         cmd,
			"invoked_emitted": false,
		}
	} else {
		props["invoked_emitted"] = true
	}

	if start, ok := app.Metadata["telemetry_start"].(time.Time); ok {
		props["duration_ms"] = time.Since(start).Milliseconds()
	}

	if err != nil {
		cat, status := telemetry.CategorizeError(err)
		props["error_category"] = cat
		props["error_message"] = err.Error()
		props["success"] = false
		if status != 0 {
			props["api_status_code"] = status
		}
		client.Capture("command_failed", props)
	} else {
		props["success"] = true
		client.Capture("command_completed", props)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	client.Shutdown(ctx)
	cancel()
}

// FinalizeTelemetry is the exported entry-point used by main.go.
func FinalizeTelemetry(app *cli.App, err error) { finalizeTelemetry(app, err) }
