// Package ui provides interactive terminal UI components.
package ui

import (
	"fmt"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/NodeOps-app/createos-cli/internal/api"
)

type catalogView int

const catalogPageSize = 20

const (
	catalogListView catalogView = iota
	catalogDetailView
	catalogConfirmPurchaseView
	catalogSearchView
)

type catalogPageLoadedMsg struct {
	skills     []api.Skill
	pagination api.Pagination
	pageNumber int
	err        error
}

type catalogPurchaseResultMsg struct {
	err         error
	purchasedID string
}

type catalogListModel struct {
	skills                []api.Skill
	cursor                int
	currentView           catalogView
	client                *api.APIClient
	pagination            api.Pagination
	pageNumber            int
	searchText            string
	purchasing            bool
	statusMsg             string
	statusErr             bool
	confirmPurchaseCursor int
	purchasedID           string
	purchasedIDsBySkillID map[string]string
	wantOpenPurchasedList bool
	searching             bool // true while a search/page load is in flight
}

func newCatalogListModel(skills []api.Skill, pagination api.Pagination, pageNumber int, searchText string, purchasedIDsBySkillID map[string]string, client *api.APIClient) catalogListModel {
	if purchasedIDsBySkillID == nil {
		purchasedIDsBySkillID = make(map[string]string)
	}
	return catalogListModel{
		skills:                skills,
		pagination:            pagination,
		pageNumber:            pageNumber,
		searchText:            searchText,
		client:                client,
		currentView:           catalogListView,
		purchasedIDsBySkillID: purchasedIDsBySkillID,
	}
}

func (m catalogListModel) Init() tea.Cmd {
	return nil
}

func loadCatalogPage(client *api.APIClient, searchText string, pageNumber int) tea.Msg {
	offset := pageNumber * catalogPageSize
	skills, pagination, err := client.ListAvailableSkillsForPurchase(searchText, offset, catalogPageSize)
	return catalogPageLoadedMsg{skills: skills, pagination: pagination, pageNumber: pageNumber, err: err}
}

func (m catalogListModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case catalogPageLoadedMsg:
		m.searching = false
		if msg.err != nil {
			m.statusErr = true
			m.statusMsg = "Error: " + msg.err.Error()
			m.currentView = catalogDetailView
			return m, nil
		}
		m.skills = msg.skills
		m.pagination = msg.pagination
		m.pageNumber = msg.pageNumber
		m.cursor = 0
		m.purchasedID = ""
		if len(m.skills) == 0 {
			m.statusMsg = "No skills on this page."
		}
		return m, nil

	case catalogPurchaseResultMsg:
		m.purchasing = false
		if msg.err != nil {
			m.statusErr = true
			m.statusMsg = "Error: " + msg.err.Error()
			m.currentView = catalogDetailView
			return m, nil
		}
		m.wantOpenPurchasedList = true
		return m, tea.Quit

	case tea.KeyMsg:
		switch m.currentView {
		case catalogListView:
			switch msg.String() {
			case "q", "ctrl+c":
				return m, tea.Quit
			case "/", "s":
				m.currentView = catalogSearchView
				return m, nil
			case "up", "k":
				if m.cursor > 0 {
					m.cursor--
				}
			case "down", "j":
				if m.cursor < len(m.skills)-1 {
					m.cursor++
				}
			case "enter":
				if len(m.skills) > 0 {
					m.currentView = catalogDetailView
					m.statusMsg = ""
					if id := m.purchasedIDsBySkillID[m.skills[m.cursor].ID]; id != "" {
						m.purchasedID = id
					}
				}
			case "n":
				if m.hasNextPage() && !m.searching {
					m.searching = true
					return m, func() tea.Msg {
						return loadCatalogPage(m.client, m.searchText, m.pageNumber+1)
					}
				}
			case "p":
				if m.hasPrevPage() && !m.searching {
					m.searching = true
					return m, func() tea.Msg {
						return loadCatalogPage(m.client, m.searchText, m.pageNumber-1)
					}
				}
			}

		case catalogSearchView:
			switch msg.String() {
			case "q", "ctrl+c":
				return m, tea.Quit
			case "esc":
				m.currentView = catalogListView
				return m, nil
			case "enter":
				m.searching = true
				m.currentView = catalogListView
				return m, func() tea.Msg {
					return loadCatalogPage(m.client, m.searchText, 0)
				}
			case "backspace":
				if len(m.searchText) > 0 {
					m.searchText = m.searchText[:len(m.searchText)-1]
				}
				return m, nil
			default:
				if len(msg.String()) == 1 {
					m.searchText += msg.String()
					return m, nil
				}
			}

		case catalogDetailView:
			switch msg.String() {
			case "q", "ctrl+c":
				return m, tea.Quit
			case "esc", "backspace":
				m.currentView = catalogListView
				m.statusMsg = ""
				m.purchasedID = ""
			case "p":
				if !m.purchasing && len(m.skills) > 0 && m.purchasedID == "" {
					m.confirmPurchaseCursor = 0
					m.currentView = catalogConfirmPurchaseView
				}
			}

		case catalogConfirmPurchaseView:
			switch msg.String() {
			case "q", "ctrl+c":
				return m, tea.Quit
			case "esc", "backspace":
				m.currentView = catalogDetailView
			case "up", "k":
				if m.confirmPurchaseCursor > 0 {
					m.confirmPurchaseCursor--
				}
			case "down", "j":
				if m.confirmPurchaseCursor < 1 {
					m.confirmPurchaseCursor++
				}
			case "enter":
				if m.confirmPurchaseCursor == 0 && len(m.skills) > 0 {
					m.purchasing = true
					m.currentView = catalogDetailView
					skill := m.skills[m.cursor]
					return m, m.purchaseCmd(skill.ID)
				}
				if m.confirmPurchaseCursor == 1 {
					m.currentView = catalogDetailView
				}
			}
		}
	}
	return m, nil
}

func (m catalogListModel) hasNextPage() bool {
	return m.pagination.Offset+m.pagination.Count < m.pagination.Total
}

func (m catalogListModel) hasPrevPage() bool {
	return m.pageNumber > 0
}

func (m catalogListModel) purchaseCmd(skillID string) tea.Cmd {
	return func() tea.Msg {
		purchasedID, err := m.client.PurchaseSkill(skillID)
		if err != nil {
			return catalogPurchaseResultMsg{err: err}
		}
		return catalogPurchaseResultMsg{purchasedID: purchasedID}
	}
}

func (m catalogListModel) View() string {
	switch m.currentView {
	case catalogDetailView:
		return m.catalogDetailRender()
	case catalogConfirmPurchaseView:
		return m.catalogConfirmPurchaseRender()
	case catalogSearchView:
		return m.catalogSearchRender()
	default:
		return m.catalogListRender()
	}
}

func formatCredits(amount float64) string {
	if amount == 1 {
		return "1 credit"
	}
	return strconv.FormatFloat(amount, 'f', 0, 64) + " credits"
}

func (m catalogListModel) catalogSearchRender() string {
	s := header("Search skills")
	s += "\n"
	s += labelStyle.Render("Search: ") + valueStyle.Render(m.searchText) + normalStyle.Render("▌") + "\n"
	s += "\n" + hintStyle.Render("Type to search by name   enter run   esc cancel   q quit")
	return s
}

func (m catalogListModel) catalogListRender() string {
	s := header("Skills Catalog")
	if m.searchText != "" {
		s += hintStyle.Render("Search: \"") + valueStyle.Render(m.searchText) + hintStyle.Render("\"") + "\n"
	}
	if m.searching {
		s += "\n" + hintStyle.Render("Searching...") + "\n"
		return s
	}
	if len(m.skills) == 0 {
		s += normalStyle.Render("No skills available.") + "\n"
		s += "\n" + hintStyle.Render("n next page   p prev page   q quit")
		return s
	}
	for i, skill := range m.skills {
		if m.cursor == i {
			s += selectedStyle.Render("▸ "+skill.Name) + "  " + hintStyle.Render(formatCredits(skill.Amount)) + "\n"
		} else {
			s += normalStyle.Render(skill.Name) + "  " + hintStyle.Render(formatCredits(skill.Amount)) + "\n"
		}
	}
	pageHint := "enter details"
	if m.hasNextPage() {
		pageHint += "   n next page"
	}
	if m.hasPrevPage() {
		pageHint += "   p prev page"
	}
	showing := m.pagination.Offset + m.pagination.Count
	total := m.pagination.Total
	if showing == 0 && len(m.skills) > 0 {
		showing = len(m.skills)
	}
	if total == 0 && len(m.skills) > 0 {
		total = len(m.skills)
	}
	s += "\n" + hintStyle.Render(fmt.Sprintf("Page %d (showing %d of %d)", m.pageNumber+1, showing, total)) + "\n"
	s += hintStyle.Render("/ or s search   ↑/↓ navigate   enter select   " + pageHint + "   q quit")
	return s
}

func (m catalogListModel) catalogDetailRender() string {
	if len(m.skills) == 0 {
		return header("") + normalStyle.Render("No skill selected.") + "\n"
	}
	skill := m.skills[m.cursor]
	overview := skill.Overview
	if overview == "" {
		overview = "-"
	}
	useCases := skill.UseCases
	if useCases == "" {
		useCases = "-"
	}
	categories := "-"
	if len(skill.Categories) > 0 {
		categories = strings.Join(skill.Categories, ", ")
	}
	s := header("")
	s += hintStyle.Render("Selected: ") + selectedStyle.Render(skill.Name) + "\n"
	s += dividerStyle.Render(strings.Repeat("─", 70)) + "\n"
	s += "\n"
	s += labelStyle.Render("Price      ") + valueStyle.Render(formatCredits(skill.Amount)) + "\n"
	s += labelStyle.Render("Categories ") + valueStyle.Render(categories) + "\n"
	s += labelStyle.Render("Created    ") + valueStyle.Render(skill.CreatedAt.Format("January 2, 2006")) + "\n"
	s += "\n"
	s += labelStyle.Render("Use cases") + "\n"
	for _, line := range strings.Split(wordWrap(useCases, 60), "\n") {
		s += normalStyle.Render(line) + "\n"
	}
	s += "\n"
	s += labelStyle.Render("Overview") + "\n"
	for _, line := range strings.Split(wordWrap(overview, 60), "\n") {
		s += normalStyle.Render(line) + "\n"
	}
	s += "\n"
	if m.purchasing {
		s += hintStyle.Render("Purchasing...") + "\n"
	} else if m.statusMsg != "" {
		if m.statusErr {
			s += errorStyle.Render(m.statusMsg) + "\n"
		} else {
			s += successStyle.Render("✓ "+m.statusMsg) + "\n"
		}
	}
	var hint string
	if m.purchasedID != "" {
		hint = "esc back   q quit"
	} else {
		hint = "p purchase   esc back   q quit"
	}
	s += "\n" + hintStyle.Render(hint)
	return s
}

func (m catalogListModel) catalogConfirmPurchaseRender() string {
	if len(m.skills) == 0 {
		return header("") + "\n"
	}
	skill := m.skills[m.cursor]
	creditsLabel := formatCredits(skill.Amount)
	options := []struct {
		label string
		i     int
	}{
		{"Confirm purchase — " + creditsLabel, 0},
		{"Cancel", 1},
	}
	s := header("Purchase " + skill.Name)
	s += "\n"
	s += labelStyle.Render("You are about to purchase:") + "\n"
	s += "  " + selectedStyle.Render(skill.Name) + "  " + valueStyle.Render(creditsLabel) + "\n"
	s += "\n"
	s += labelStyle.Render("Choose an option:") + "\n\n"
	for _, opt := range options {
		if m.confirmPurchaseCursor == opt.i {
			s += selectedStyle.Render("▸ "+opt.label) + "\n"
		} else {
			s += normalStyle.Render("  "+opt.label) + "\n"
		}
	}
	s += "\n" + hintStyle.Render("↑/↓ select   enter confirm   esc back   q quit")
	return s
}

// RunCatalogList runs the catalog TUI. On successful purchase, opens the purchased skills list.
func RunCatalogList(skills []api.Skill, pagination api.Pagination, pageNumber int, searchText string, purchasedIDsBySkillID map[string]string, client *api.APIClient) error {
	p := tea.NewProgram(newCatalogListModel(skills, pagination, pageNumber, searchText, purchasedIDsBySkillID, client), tea.WithAltScreen())
	finalModel, err := p.Run()
	if err != nil {
		return err
	}
	if m, ok := finalModel.(catalogListModel); ok && m.wantOpenPurchasedList && m.client != nil {
		items, err := m.client.ListMyPurchasedSkills()
		if err != nil {
			return err
		}
		if len(items) > 0 {
			return RunSkillsList(items, m.client)
		}
	}
	return nil
}
