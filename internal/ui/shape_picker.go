package ui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/NodeOps-app/createos-cli/internal/api"
)

// PickShape runs an interactive bubbletea picker over the supplied
// shape catalog and returns the picked shape's id. Returns "" if the
// user quit without selecting (ctrl+c / esc / q). Caller is expected
// to handle the empty-string outcome.
//
// Use only when stdout is a TTY (see internal/terminal.IsInteractive).
// Headless callers should print a hint instead.
func PickShape(shapes []api.Shape) (string, error) {
	if len(shapes) == 0 {
		return "", fmt.Errorf("no sandbox sizes available")
	}
	m := shapePickerModel{shapes: shapes, cursor: 0}
	p := tea.NewProgram(m)
	finalModel, err := p.Run()
	if err != nil {
		return "", err
	}
	out := finalModel.(shapePickerModel)
	if out.quit {
		return "", nil
	}
	return out.shapes[out.cursor].ID, nil
}

// shapePickerModel is the bubbletea Model for the shape picker.
type shapePickerModel struct {
	shapes []api.Shape
	cursor int
	quit   bool
	done   bool
}

func (m shapePickerModel) Init() tea.Cmd { return nil }

func (m shapePickerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q", "esc":
			m.quit = true
			return m, tea.Quit
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if m.cursor < len(m.shapes)-1 {
				m.cursor++
			}
		case "enter", " ":
			m.done = true
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m shapePickerModel) View() string {
	if m.done || m.quit {
		// Clear the picker once we're done so it doesn't linger above
		// the rest of the create output.
		return ""
	}
	var b strings.Builder
	b.WriteString(titleStyle.Render("Pick a sandbox size") + "\n")
	b.WriteString(hintStyle.Render("Arrow keys to move, enter to pick, q to cancel") + "\n\n")

	// Column widths from the data.
	idW, cpuW, memW, diskW := 4, 4, 6, 8
	for _, s := range m.shapes {
		idW = max(idW, len(s.ID))
		cpuW = max(cpuW, len(fmt.Sprintf("%d", s.VCPU)))
		memW = max(memW, len(humanMiB(int64(s.MemMib))))
		diskW = max(diskW, len(humanMiB(s.DefaultDiskMib)))
	}

	header := fmt.Sprintf("  %-*s  %-*s  %-*s  %-*s",
		idW, "ID", cpuW, "VCPU", memW, "RAM", diskW, "DISK")
	b.WriteString(labelStyle.Render(header) + "\n")

	for i, s := range m.shapes {
		row := fmt.Sprintf("%-*s  %-*d  %-*s  %-*s",
			idW, s.ID, cpuW, s.VCPU, memW, humanMiB(int64(s.MemMib)), diskW, humanMiB(s.DefaultDiskMib))
		if i == m.cursor {
			b.WriteString(selectedStyle.Render("› "+row) + "\n")
		} else {
			b.WriteString(normalStyle.Render("  "+row) + "\n")
		}
	}
	return b.String()
}

// humanMiB renders a MiB value as GB / MB. Avoids importing a heavy
// units library — fc-spawn shapes only span 256 MiB to 60 GB.
func humanMiB(mib int64) string {
	if mib <= 0 {
		return "—"
	}
	if mib >= 1024 && mib%1024 == 0 {
		return fmt.Sprintf("%d GB", mib/1024)
	}
	if mib >= 1024 {
		return fmt.Sprintf("%.1f GB", float64(mib)/1024)
	}
	return fmt.Sprintf("%d MB", mib)
}

// Local lipgloss palette mirrors the package's existing skill picker
// styles so a user sees one coherent look across pickers.
var (
	// Reuse the already-declared package vars from skills_list.go.
	_ = titleStyle
	_ = hintStyle
	_ = labelStyle
	_ = selectedStyle
	_ = normalStyle
)

// max in Go 1.21+ stdlib — written explicitly here so we don't depend
// on the build toolchain having generics-friendly builtins enabled.
func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// Compile-time guard so we notice if lipgloss is removed upstream.
var _ = lipgloss.NewStyle
