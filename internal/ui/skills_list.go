package ui

import (
	"fmt"
	"strings"

	"github.com/NodeOps-app/createos-cli/internal/api"
	"github.com/NodeOps-app/createos-cli/internal/installer"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

const asciiLogo = `
 ██████╗██████╗ ███████╗ █████╗ ████████╗███████╗ ██████╗ ███████╗
██╔════╝██╔══██╗██╔════╝██╔══██╗╚══██╔══╝██╔════╝██╔═══██╗██╔════╝
██║     ██████╔╝█████╗  ███████║   ██║   █████╗  ██║   ██║███████╗
██║     ██╔══██╗██╔══╝  ██╔══██║   ██║   ██╔══╝  ██║   ██║╚════██║
╚██████╗██║  ██║███████╗██║  ██║   ██║   ███████╗╚██████╔╝███████║
 ╚═════╝╚═╝  ╚═╝╚══════╝╚═╝  ╚═╝  ╚═╝   ╚══════╝ ╚═════╝ ╚══════╝`

var (
	dividerStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	logoStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("6"))
	titleStyle    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("15"))
	selectedStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("6")).Bold(true)
	normalStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("7"))
	hintStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	labelStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	valueStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("15"))
	successStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
	errorStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
)

type view int

const (
	listView view = iota
	detailView
	installMenuView
	confirmUninstallView
)

type installResultMsg struct {
	err error
}

type uninstallResultMsg struct {
	err error
}

type skillsListModel struct {
	items         []api.PurchasedSkillItem
	cursor        int
	currentView   view
	client        *api.ApiClient
	installing    bool
	statusMsg     string
	statusErr     bool
	installCursor int
	pendingScope  installer.InstallScope
}

func newSkillsListModel(items []api.PurchasedSkillItem, client *api.ApiClient) skillsListModel {
	return skillsListModel{items: items, currentView: listView, client: client}
}

func (m skillsListModel) Init() tea.Cmd {
	return nil
}

func (m skillsListModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case installResultMsg:
		m.installing = false
		if msg.err != nil {
			m.statusErr = true
			m.statusMsg = "Error: " + msg.err.Error()
		} else {
			m.statusErr = false
			m.statusMsg = "Installed successfully"
		}
		m.currentView = detailView
		return m, nil

	case uninstallResultMsg:
		m.installing = false
		if msg.err != nil {
			m.statusErr = true
			m.statusMsg = "Error: " + msg.err.Error()
		} else {
			m.statusErr = false
			m.statusMsg = "Uninstalled successfully"
		}
		m.currentView = detailView
		return m, nil

	case tea.KeyMsg:
		switch m.currentView {
		case listView:
			switch msg.String() {
			case "q", "ctrl+c":
				return m, tea.Quit
			case "up", "k":
				if m.cursor > 0 {
					m.cursor--
				}
			case "down", "j":
				if m.cursor < len(m.items)-1 {
					m.cursor++
				}
			case "enter":
				m.currentView = detailView
				m.statusMsg = ""
			}

		case detailView:
			switch msg.String() {
			case "q", "ctrl+c":
				return m, tea.Quit
			case "esc", "backspace":
				m.currentView = listView
				m.statusMsg = ""
			case "i":
				if !m.installing {
					m.installCursor = 0
					m.currentView = installMenuView
				}
			}

		case installMenuView:
			switch msg.String() {
			case "q", "ctrl+c":
				return m, tea.Quit
			case "esc", "backspace":
				m.currentView = detailView
			case "up", "k":
				if m.installCursor > 0 {
					m.installCursor--
				}
			case "down", "j":
				if m.installCursor < 1 {
					m.installCursor++
				}
			case "enter":
				item := m.items[m.cursor]
				scope := installer.InstallScope(m.installCursor)
				if installer.IsScopeInstalled(item.Skill.UniqueName, scope) {
					m.pendingScope = scope
					m.currentView = confirmUninstallView
					return m, nil
				}
				m.installing = true
				m.currentView = detailView
				return m, m.installCmd(item, scope)
			}

		case confirmUninstallView:
			switch msg.String() {
			case "q", "ctrl+c":
				return m, tea.Quit
			case "esc", "backspace", "n":
				m.currentView = installMenuView
			case "y", "enter":
				m.installing = true
				m.currentView = detailView
				item := m.items[m.cursor]
				return m, m.uninstallCmd(item, m.pendingScope)
			}
		}
	}
	return m, nil
}

func (m skillsListModel) uninstallCmd(item api.PurchasedSkillItem, scope installer.InstallScope) tea.Cmd {
	return func() tea.Msg {
		err := installer.UninstallScope(item.Skill.UniqueName, scope)
		return uninstallResultMsg{err: err}
	}
}

func (m skillsListModel) installCmd(item api.PurchasedSkillItem, scope installer.InstallScope) tea.Cmd {
	return func() tea.Msg {
		url, err := m.client.GetSkillDownloadUrl(item.Id)
		if err != nil {
			return installResultMsg{err: err}
		}
		_, err = installer.InstallToScope(url, item.Skill.UniqueName, scope)
		return installResultMsg{err: err}
	}
}

func (m skillsListModel) View() string {
	switch m.currentView {
	case detailView:
		return m.detailViewRender()
	case installMenuView:
		return m.installMenuRender()
	case confirmUninstallView:
		return m.confirmUninstallRender()
	default:
		return m.listViewRender()
	}
}

func header(subtitle string) string {
	var s string
	for _, line := range strings.Split(asciiLogo, "\n") {
		s += logoStyle.Render(line) + "\n"
	}
	s += dividerStyle.Render(strings.Repeat("─", 70)) + "\n"
	if subtitle != "" {
		s += titleStyle.Render(subtitle) + "\n"
		s += dividerStyle.Render(strings.Repeat("─", 70)) + "\n"
	}
	return s
}

func (m skillsListModel) listViewRender() string {
	s := header("Your Purchased Skills")
	for i, item := range m.items {
		if m.cursor == i {
			s += selectedStyle.Render("  ▸ "+item.Skill.Name) + "\n"
		} else {
			s += normalStyle.Render("    "+item.Skill.Name) + "\n"
		}
	}
	s += "\n" + hintStyle.Render("  ↑/↓ navigate   enter select   q quit")
	return s
}

func (m skillsListModel) detailViewRender() string {
	skill := m.items[m.cursor].Skill

	overview := skill.Overview
	if overview == "" {
		overview = "-"
	}

	categories := "-"
	if len(skill.Categories) > 0 {
		categories = strings.Join(skill.Categories, ", ")
	}

	s := header(skill.Name)
	s += labelStyle.Render("  Categories  ") + valueStyle.Render(categories) + "\n"
	s += labelStyle.Render("  Created     ") + valueStyle.Render(skill.CreatedAt.Format("January 2, 2006")) + "\n\n"
	s += labelStyle.Render("  Overview") + "\n"
	s += normalStyle.Render("  "+wordWrap(overview, 60)) + "\n"

	if m.installing {
		s += "\n" + hintStyle.Render("  Installing...")
	} else if m.statusMsg != "" {
		if m.statusErr {
			s += "\n" + errorStyle.Render("  "+m.statusMsg)
		} else {
			s += "\n" + successStyle.Render("  ✓ "+m.statusMsg)
		}
	}

	s += "\n" + hintStyle.Render("  i install   esc back   q quit")
	return s
}

func (m skillsListModel) installMenuRender() string {
	skill := m.items[m.cursor].Skill

	scopes := []struct {
		label string
		scope installer.InstallScope
	}{
		{"Local  — current project", installer.ScopeLocal},
		{"Global — home directory", installer.ScopeGlobal},
	}

	s := header("Install " + skill.Name)
	s += labelStyle.Render("  Where do you want to install?") + "\n\n"

	for i, sc := range scopes {
		installed := installer.IsScopeInstalled(skill.UniqueName, sc.scope)
		suffix := ""
		if installed {
			suffix = hintStyle.Render("  (installed)")
		}
		if m.installCursor == i {
			s += selectedStyle.Render("  ▸ "+sc.label) + suffix + "\n"
		} else {
			s += normalStyle.Render("    "+sc.label) + suffix + "\n"
		}
	}

	scope := installer.InstallScope(m.installCursor)
	action := "enter install"
	if installer.IsScopeInstalled(skill.UniqueName, scope) {
		action = "enter delete"
	}
	s += "\n" + hintStyle.Render(fmt.Sprintf("  ↑/↓ navigate   %s   esc back", action))
	return s
}

func (m skillsListModel) confirmUninstallRender() string {
	skill := m.items[m.cursor].Skill
	scopeLabel := "Local"
	if m.pendingScope == installer.ScopeGlobal {
		scopeLabel = "Global"
	}

	s := header("Uninstall " + skill.Name)
	s += normalStyle.Render("  Remove ") + errorStyle.Render(scopeLabel) + normalStyle.Render(" installation of ") + selectedStyle.Render(skill.Name) + normalStyle.Render("?") + "\n\n"
	s += hintStyle.Render("  y confirm   n cancel")
	return s
}

func wordWrap(text string, width int) string {
	if len(text) <= width {
		return text
	}
	var lines []string
	for len(text) > width {
		breakAt := strings.LastIndex(text[:width], " ")
		if breakAt <= 0 {
			breakAt = width
		}
		lines = append(lines, text[:breakAt])
		text = strings.TrimLeft(text[breakAt:], " ")
	}
	if text != "" {
		lines = append(lines, text)
	}
	return strings.Join(lines, "\n")
}

// RunSkillsList renders the interactive skills list
func RunSkillsList(items []api.PurchasedSkillItem, client *api.ApiClient) error {
	p := tea.NewProgram(newSkillsListModel(items, client), tea.WithAltScreen())
	_, err := p.Run()
	return err
}
