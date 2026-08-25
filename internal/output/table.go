package output

import (
	"os"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/pterm/pterm"
	"golang.org/x/term"
)

const (
	defaultTableWidth = 120
	minColumnWidth    = 4
	tableSeparator    = "  "
	tableWidthPadding = 2
)

// RenderTable prints a pterm table constrained to the current terminal width.
// Long cells are shortened before rendering so rows do not wrap in narrow panes.
func RenderTable(data pterm.TableData) error {
	return pterm.DefaultTable.
		WithHasHeader().
		WithSeparator(tableSeparator).
		WithData(FitTable(data, TerminalWidth())).
		Render()
}

// TerminalWidth returns the usable width for human-facing terminal output.
func TerminalWidth() int {
	if raw := strings.TrimSpace(os.Getenv("COLUMNS")); raw != "" {
		width, err := strconv.Atoi(raw)
		if err == nil && width > 0 {
			return usableTableWidth(width)
		}
	}

	width, _, err := term.GetSize(int(os.Stdout.Fd())) // #nosec G115 -- stdout fd is a small well-known descriptor
	if err != nil || width <= 0 {
		return defaultTableWidth
	}
	return usableTableWidth(width)
}

func usableTableWidth(width int) int {
	if width > tableWidthPadding {
		return width - tableWidthPadding
	}
	return width
}

// FitTable returns a copy of data with cell contents shortened to fit width.
func FitTable(data pterm.TableData, width int) pterm.TableData {
	if len(data) == 0 || width <= 0 {
		return data
	}

	cols := maxColumns(data)
	if cols == 0 {
		return data
	}

	maxWidths := columnWidths(data, cols)
	if tableWidth(maxWidths) <= width {
		return cloneTable(data)
	}

	minWidths := minimumWidths(data[0], maxWidths)
	targets := append([]int(nil), maxWidths...)
	for tableWidth(targets) > width {
		idx := widestReducibleColumn(targets, minWidths)
		if idx < 0 {
			break
		}
		targets[idx]--
	}

	fitted := make(pterm.TableData, len(data))
	for i, row := range data {
		fitted[i] = make([]string, len(row))
		for j, cell := range row {
			if j >= len(targets) {
				fitted[i][j] = cell
				continue
			}
			fitted[i][j] = truncateCell(cell, targets[j])
		}
	}
	return fitted
}

func cloneTable(data pterm.TableData) pterm.TableData {
	out := make(pterm.TableData, len(data))
	for i, row := range data {
		out[i] = append([]string(nil), row...)
	}
	return out
}

func maxColumns(data pterm.TableData) int {
	cols := 0
	for _, row := range data {
		if len(row) > cols {
			cols = len(row)
		}
	}
	return cols
}

func columnWidths(data pterm.TableData, cols int) []int {
	widths := make([]int, cols)
	for _, row := range data {
		for i, cell := range row {
			if w := cellWidth(cell); w > widths[i] {
				widths[i] = w
			}
		}
	}
	return widths
}

func minimumWidths(header []string, maxWidths []int) []int {
	mins := make([]int, len(maxWidths))
	for i, maxWidth := range maxWidths {
		headerWidth := 0
		if i < len(header) {
			headerWidth = cellWidth(header[i])
		}
		min := minColumnWidth
		if headerWidth > min {
			min = headerWidth
		}
		if min > maxWidth {
			min = maxWidth
		}
		mins[i] = min
	}
	return mins
}

func widestReducibleColumn(widths, mins []int) int {
	idx := -1
	for i, width := range widths {
		if width <= mins[i] {
			continue
		}
		if idx == -1 || width > widths[idx] {
			idx = i
		}
	}
	return idx
}

func tableWidth(widths []int) int {
	total := 0
	for _, width := range widths {
		total += width
	}
	if len(widths) > 1 {
		total += (len(widths) - 1) * len(tableSeparator)
	}
	return total
}

func cellWidth(s string) int {
	width := 0
	for _, line := range strings.Split(s, "\n") {
		if w := utf8.RuneCountInString(line); w > width {
			width = w
		}
	}
	return width
}

func truncateCell(s string, width int) string {
	if width <= 0 || cellWidth(s) <= width {
		return s
	}
	s = strings.ReplaceAll(s, "\n", " ")
	if width <= 3 {
		return firstRunes(s, width)
	}
	return firstRunes(s, width-3) + "..."
}

func firstRunes(s string, n int) string {
	if n <= 0 {
		return ""
	}
	for i := range s {
		if n == 0 {
			return s[:i]
		}
		n--
	}
	return s
}
