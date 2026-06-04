package ui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// PickerItem is one row in a generic picker. Title is the highlight
// label; Subtitle is a dim secondary line shown in parentheses; Value
// is what gets returned when the user hits enter (typically the id).
type PickerItem struct {
	Title    string
	Subtitle string
	Value    string
}

// Pick runs a single-column bubbletea picker over items. Returns the
// chosen item's Value, or "" if the user cancelled.
func Pick(title string, items []PickerItem) (string, error) {
	if len(items) == 0 {
		return "", fmt.Errorf("nothing to pick from")
	}
	m := pickerModel{title: title, items: items}
	p := tea.NewProgram(m)
	out, err := p.Run()
	if err != nil {
		return "", err
	}
	res, ok := out.(pickerModel)
	if !ok {
		return "", fmt.Errorf("unexpected picker result")
	}
	if res.quit {
		return "", nil
	}
	return res.items[res.cursor].Value, nil
}

type pickerModel struct {
	title  string
	items  []PickerItem
	cursor int
	quit   bool
	done   bool
}

func (m pickerModel) Init() tea.Cmd { return nil }

func (m pickerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if msg, ok := msg.(tea.KeyMsg); ok {
		switch msg.String() {
		case "ctrl+c", "q", "esc":
			m.quit = true
			return m, tea.Quit
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if m.cursor < len(m.items)-1 {
				m.cursor++
			}
		case "enter", " ":
			m.done = true
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m pickerModel) View() string {
	if m.done || m.quit {
		return ""
	}
	var b strings.Builder
	b.WriteString(titleStyle.Render(m.title) + "\n")
	b.WriteString(hintStyle.Render("Arrow keys to move, enter to pick, q to cancel") + "\n\n")
	for i, item := range m.items {
		line := item.Title
		if item.Subtitle != "" {
			line += "   " + item.Subtitle
		}
		if i == m.cursor {
			b.WriteString(selectedStyle.Render("› "+line) + "\n")
		} else {
			b.WriteString(normalStyle.Render("  "+line) + "\n")
		}
	}
	return b.String()
}
