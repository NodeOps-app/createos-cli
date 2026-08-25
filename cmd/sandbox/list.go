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
  createos sandbox list --quiet

  # Include lower-priority columns
  createos sandbox list --wide`,
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
			&cli.BoolFlag{
				Name:  "wide",
				Usage: "Show extra columns",
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

	var rows []api.SandboxView

	if !showAll && status == "" {
		// Fetch each active status individually so paused / resuming VMs
		// aren't pushed out of the first page by destroyed rows.
		activeStatuses := []string{"running", "paused", "pausing", "resuming", "forking"}
		for _, s := range activeStatuses {
			page, _, err := client.ListSandboxes(c.Context, api.ListSandboxesOpts{
				Limit:  c.Int("limit"),
				Offset: c.Int("offset"),
				Status: s,
			})
			if err != nil {
				return err
			}
			rows = append(rows, page...)
		}
	} else {
		var err error
		rows, _, err = client.ListSandboxes(c.Context, api.ListSandboxesOpts{
			Limit:  c.Int("limit"),
			Offset: c.Int("offset"),
			Status: status,
		})
		if err != nil {
			return err
		}
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
		tableData := sandboxListTable(rows, output.TerminalWidth(), c.Bool("wide"))
		_ = output.RenderTable(tableData) //nolint:errcheck
	})
	return nil
}

func sandboxListTable(rows []api.SandboxView, width int, wide bool) pterm.TableData {
	columns := []string{"ID", "Name", "Status", "Size", "IP"}
	if wide {
		columns = append(columns, "Created")
	}
	switch {
	case width < 70:
		columns = []string{"Name", "Status", "Size"}
	case width < 90:
		columns = []string{"ID", "Name", "Status", "Size"}
	}

	tableData := make(pterm.TableData, 0, 1+len(rows))
	tableData = append(tableData, columns)
	for _, r := range rows {
		values := map[string]string{
			"ID":      r.ID,
			"Name":    strOrDash(r.Name),
			"Status":  r.Status,
			"Size":    r.Shape,
			"IP":      ptrOrDash(r.IP),
			"Created": r.CreatedAt.Local().Format("2006-01-02 15:04"),
		}
		row := make([]string, 0, len(columns))
		for _, col := range columns {
			row = append(row, values[col])
		}
		tableData = append(tableData, row)
	}
	return tableData
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
