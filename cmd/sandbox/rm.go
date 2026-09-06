package sandbox

import (
	"fmt"
	"os"
	"strings"

	"github.com/pterm/pterm"
	"github.com/urfave/cli/v2"

	"github.com/NodeOps-app/createos-cli/internal/api"
	"github.com/NodeOps-app/createos-cli/internal/terminal"
)

func newRmCommand() *cli.Command {
	return &cli.Command{
		Name:      "rm",
		Aliases:   []string{"delete", "destroy"},
		Usage:     "Delete one or more sandboxes",
		ArgsUsage: "[<sandbox-id> …]",
		Description: `Delete one or more sandboxes. Teardown is irreversible.

Examples:
  # Delete two specific sandboxes
  createos sandbox rm sb-01k... sb-01k...

  # Pipe IDs from list
  createos sandbox list --quiet --status failed | xargs createos sandbox rm --force

  # In a script — non-interactive, must pass --force
  createos sandbox rm sb-01k... --force`,
		Flags: []cli.Flag{
			&cli.BoolFlag{
				Name:    "force",
				Aliases: []string{"y", "yes"},
				Usage:   "Skip the confirmation prompt (required in non-interactive mode)",
			},
		},
		Action: runRm,
	}
}

func runRm(c *cli.Context) error {
	client, ok := c.App.Metadata[api.SandboxClientKey].(*api.SandboxClient)
	if !ok {
		return fmt.Errorf("you're not signed in — run 'createos login' to get started")
	}

	// Collect ids from positionals AND pick up the --force/-y flag even
	// when it lands after a positional (urfave/cli v2 stops flag parsing
	// at the first positional, so `rm SB1 --force` wouldn't otherwise
	// be honoured). Strip recognised flag tokens from the id list.
	args := c.Args().Slice()
	ids := make([]string, 0, len(args))
	forceFromArgs := false
	for _, a := range args {
		a = strings.TrimSpace(a)
		switch a {
		case "":
			continue
		case "--force", "-y", "--yes", "-yes":
			forceFromArgs = true
			continue
		}
		ids = append(ids, a)
	}

	if len(ids) == 0 {
		// Interactive: let the user pick from their running + paused
		// sandboxes. Non-interactive: error early so scripts don't hang.
		if !terminal.IsInteractive() {
			return fmt.Errorf("please provide at least one sandbox ID\n\n  To see your sandboxes and their IDs, run:\n    createos sandbox list")
		}
		picked, err := pickSandboxesForDelete(c, client)
		if err != nil {
			return err
		}
		ids = picked
		if len(ids) == 0 {
			fmt.Println("Cancelled. No sandboxes were deleted.")
			return nil
		}
	}

	force := c.Bool("force") || forceFromArgs
	if !terminal.IsInteractive() && !force {
		return fmt.Errorf("non-interactive mode: use --force to confirm deletion\n\n  Example:\n    createos sandbox rm %s --force", ids[0])
	}

	if terminal.IsInteractive() && !force {
		confirm, err := pterm.DefaultInteractiveConfirm.
			WithDefaultText(confirmText(ids)).
			WithDefaultValue(false).
			Show()
		if err != nil {
			return fmt.Errorf("could not read confirmation: %w", err)
		}
		if !confirm {
			fmt.Println("Cancelled. No sandboxes were deleted.")
			return nil
		}
	}

	// Resolve any names to IDs before deleting. Resolution failures
	// are reported per-ref so a typo in one name doesn't kill the rest
	// of the batch.
	failed := 0
	for _, ref := range ids {
		id, err := resolveSandboxRef(c.Context, client, ref)
		if err != nil {
			pterm.Error.Printfln("%s: %s", ref, api.UserMessageVerbose(err))
			failed++
			continue
		}
		if err := client.DestroySandbox(c.Context, id); err != nil {
			pterm.Error.Printfln("%s: %s", ref, api.UserMessageVerbose(err))
			failed++
			continue
		}
		// Echo the friendly ref the user typed; if it was already an
		// id this reads the same, if it was a name they see what was
		// actually removed.
		if id != ref {
			pterm.Success.Printfln("Deleted %s (%s)", ref, id)
		} else {
			pterm.Success.Printfln("Deleted %s", id)
		}
	}
	if failed > 0 {
		// Non-zero exit so scripts can tell something went wrong.
		os.Exit(1)
	}
	return nil
}

// confirmText keeps the prompt short for one sandbox and explicit for many.
func confirmText(ids []string) string {
	if len(ids) == 1 {
		return fmt.Sprintf("Permanently delete sandbox %s?", ids[0])
	}
	return fmt.Sprintf("Permanently delete %d sandboxes?", len(ids))
}

// pickSandboxesForDelete shows a checkbox list of the user's running +
// paused sandboxes and returns the picked IDs. Headless callers never
// reach this — the caller bails earlier.
func pickSandboxesForDelete(c *cli.Context, client *api.SandboxClient) ([]string, error) {
	// Pull both states; the API only takes one ?status= at a time so do two calls.
	var rows []api.SandboxView
	for _, st := range []string{api.SandboxStatusRunning, api.SandboxStatusPaused} {
		page, _, err := client.ListSandboxes(c.Context, api.ListSandboxesOpts{
			Limit: 200, Status: st,
		})
		if err != nil {
			return nil, err
		}
		rows = append(rows, page...)
	}
	if len(rows) == 0 {
		fmt.Println("You don't have any running or paused sandboxes to delete.")
		return nil, nil
	}

	options := make([]string, 0, len(rows))
	idByOption := make(map[string]string, len(rows))
	for _, r := range rows {
		var label string
		if r.Name != nil && *r.Name != "" {
			label = *r.Name + "   " + r.ID + "   " + r.Status
		} else {
			label = r.ID + "   " + r.Status
		}
		options = append(options, label)
		idByOption[label] = r.ID
	}

	picked, err := multiselect("Pick sandboxes to delete (space = select, enter = confirm)").
		WithOptions(options).
		Show()
	if err != nil {
		return nil, fmt.Errorf("could not read your selection: %w", err)
	}
	out := make([]string, 0, len(picked))
	for _, p := range picked {
		if id, ok := idByOption[p]; ok {
			out = append(out, id)
		}
	}
	return out, nil
}
