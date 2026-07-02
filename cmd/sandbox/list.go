package sandbox

import (
	"fmt"
	"sort"

	"github.com/pterm/pterm"
	"github.com/urfave/cli/v2"

	"github.com/NodeOps-app/createos-cli/internal/api"
	"github.com/NodeOps-app/createos-cli/internal/output"
)

func newListCommand() *cli.Command {
	return &cli.Command{
		Name:    "list",
		Aliases: []string{"ls"},
		Usage:   "List your sandboxes",
		Description: `Show your sandboxes. By default active ones are shown (running, paused, pausing, resuming, forking).

Examples:
  # Active sandboxes (running, paused, pausing, resuming, forking)
  createos sandbox list

  # Every sandbox, including destroyed / failed rows
  createos sandbox list --all

  # Just one status
  createos sandbox list --status paused

  # Pipe-friendly: IDs only
  createos sandbox list --quiet`,
		Flags: []cli.Flag{
			&cli.IntFlag{
				Name:  "limit",
				Value: 50,
				Usage: "How many sandboxes to show",
			},
			&cli.IntFlag{
				Name:  "offset",
				Value: 0,
				Usage: "Skip the first N sandboxes (for paging)",
			},
			&cli.StringFlag{
				Name:  "status",
				Usage: "Show only sandboxes in this state (running | paused | pausing | resuming | forking | creating | destroyed | failed)",
			},
			&cli.BoolFlag{
				Name:    "all",
				Aliases: []string{"a"},
				Usage:   "Show sandboxes in every state, not just active ones",
			},
			&cli.BoolFlag{
				Name:    "quiet",
				Aliases: []string{"q"},
				Usage:   "Show only the IDs (great for scripting)",
			},
		},
		Action: runList,
	}
}

func runList(c *cli.Context) error {
	client, ok := c.App.Metadata[api.SandboxClientKey].(*api.SandboxClient)
	if !ok {
		return fmt.Errorf("you're not signed in — run 'createos login' to get started")
	}

	// Default: active statuses only. --all clears the filter. --status overrides both.
	status := ""
	showAll := c.Bool("all")
	if explicit := c.String("status"); explicit != "" {
		status = explicit
	}

	rows, _, err := client.ListSandboxes(c.Context, api.ListSandboxesOpts{
		Limit:  c.Int("limit"),
		Offset: c.Int("offset"),
		Status: status,
	})
	if err != nil {
		return err
	}

	// When neither --all nor --status is given, keep only active statuses.
	if !showAll && status == "" {
		activeStatuses := map[string]bool{
			"running":  true,
			"paused":   true,
			"pausing":  true,
			"resuming": true,
			"forking":  true,
		}
		filtered := rows[:0]
		for _, r := range rows {
			if activeStatuses[r.Status] {
				filtered = append(filtered, r)
			}
		}
		rows = filtered
	}

	sort.SliceStable(rows, func(i, j int) bool {
		return rows[i].CreatedAt.After(rows[j].CreatedAt)
	})

	// --quiet always wins, regardless of TTY / --output. Scripts that
	// pipe `createos sandbox list --quiet | xargs ...` get plain IDs.
	if c.Bool("quiet") {
		for _, r := range rows {
			fmt.Println(r.ID)
		}
		return nil
	}

	output.Render(c, rows, func() {
		if len(rows) == 0 {
			switch {
			case status == "" && showAll:
				fmt.Println("You don't have any sandboxes yet.")
			case status == "":
				fmt.Println("You don't have any active sandboxes.")
			default:
				fmt.Printf("You don't have any %s sandboxes.\n", status)
			}
			fmt.Println()
			pterm.Println(pterm.Gray("  Create one with: createos sandbox create"))
			return
		}
		tableData := pterm.TableData{
			{"ID", "Name", "Status", "Size", "IP", "Created"},
		}
		for _, r := range rows {
			tableData = append(tableData, []string{
				r.ID,
				strOrDash(r.Name),
				r.Status,
				r.Shape,
				ptrOrDash(r.IP),
				r.CreatedAt.Format("2006-01-02 15:04"),
			})
		}
		_ = pterm.DefaultTable.WithHasHeader().WithData(tableData).Render() //nolint:errcheck
	})
	return nil
}

// strOrDash collapses a nullable pointer to "-" when empty so the
// table doesn't show ugly blank cells.
func strOrDash(s *string) string {
	if s == nil || *s == "" {
		return "-"
	}
	return *s
}

// ptrOrDash mirrors strOrDash for any *string field. Kept as a
// separate name so the call sites read clearly.
func ptrOrDash(s *string) string { return strOrDash(s) }
