package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/renderorange/chute/config"
	"github.com/renderorange/chute/mail"
)

// searchFuncs runs a server-side search for one folder.
type searchFuncs func(acct config.Account, folder, query string) ([]mail.Message, error)

// searchMsg reports a server search.
type searchMsg struct {
	account  string
	folder   string
	query    string
	messages []mail.Message
	err      error
}

// beginSearch opens the server-search prompt. `/` keeps the local filter; a search is the
// only way to find mail the folder load did not return (#68).
func (m Model) beginSearch() (tea.Model, tea.Cmd) {
	if m.openFolder == "" {
		m.status = "open a folder to search"
		return m, nil
	}
	if m.searchMessages == nil {
		return m, nil
	}

	m.searchOpen = true
	m.searchQuery = ""
	m.status = ""

	return m, nil
}

// handleSearchKey edits the prompt. Every printable key belongs to the query.
func (m Model) handleSearchKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyCtrlC:
		return m, tea.Quit

	case tea.KeyEsc:
		m.searchOpen = false
		m.searchQuery = ""
		m.status = ""

		return m, nil

	case tea.KeyEnter:
		query := strings.TrimSpace(m.searchQuery)
		m.searchOpen = false
		m.searchQuery = ""

		// An empty query is rejected here rather than sent: SEARCH with no criteria
		// matches the whole folder and looks like a reload.
		if query == "" {
			m.status = "empty search"
			return m, nil
		}

		return m, m.searchCmd(query)

	case tea.KeyBackspace:
		if runes := []rune(m.searchQuery); len(runes) > 0 {
			m.searchQuery = string(runes[:len(runes)-1])
		}

		return m, nil

	case tea.KeyRunes:
		m.searchQuery += string(msg.Runes)

		return m, nil

	case tea.KeySpace:
		m.searchQuery += " "

		return m, nil
	}

	return m, nil
}

// searchCmd runs the search in the background.
func (m Model) searchCmd(query string) tea.Cmd {
	if m.searchMessages == nil || m.openFolder == "" || len(m.cfg.Accounts) == 0 {
		return nil
	}

	acct := m.cfg.Accounts[m.account]
	folder := m.openFolder
	search := m.searchMessages

	return func() tea.Msg {
		messages, err := search(acct, folder, query)

		return searchMsg{account: acct.Name, folder: folder, query: query, messages: messages, err: err}
	}
}

// applySearch replaces the visible list with the matches, or says why it did not. A result
// list is transient: the folder's own messages stay in m.messages, and esc restores them.
func (m *Model) applySearch(msg searchMsg) {
	if msg.account != m.accountName() || msg.folder != m.openFolder {
		return
	}

	if msg.err != nil {
		m.status = statusText(msg.err)
		return
	}
	if len(msg.messages) == 0 {
		m.status = "no matches"
		return
	}

	m.searchReturnCursor = m.messageCursor
	m.searchReturnScroll = m.messageScroll
	m.searchResults = sortNewestFirst(msg.messages)
	m.searchActive = true
	m.messageCursor = 0
	m.messageScroll = 0
	m.readerScroll = 0
	m.visualAnchor = noVisualAnchor

	// No thread context: a parent may not be in the result set, so a depth would lie.
	m.rows = make([]Row, len(m.searchResults))
	for i, message := range m.searchResults {
		m.rows[i] = Row{Message: message}
	}

	m.status = fmt.Sprintf("%s found", countLabel(len(m.rows), "message"))
}

// leaveSearch restores the folder's loaded list and the cursor it had before the search.
func (m *Model) leaveSearch() {
	m.searchActive = false
	m.searchResults = nil
	m.rebuildRows()

	if len(m.rows) > 0 {
		m.messageCursor = clamp(m.searchReturnCursor, 0, len(m.rows)-1)
	} else {
		m.messageCursor = 0
	}
	m.messageScroll = m.searchReturnScroll
	m.clampMessageScroll()
	m.status = ""
}

// clearSearch drops the prompt and the results. Anything that replaces the folder's contents
// calls it, so a result list never survives into another folder or account.
func (m *Model) clearSearch() {
	m.searchOpen = false
	m.searchQuery = ""
	m.searchActive = false
	m.searchResults = nil
	m.searchReturnCursor = 0
	m.searchReturnScroll = 0
}

// resultsOnly refuses a folder-changing action while the list shows search results: the UID
// set may no longer match what the folder view holds, and deleting the wrong mail is the one
// surprise worth preventing.
func (m Model) resultsOnly() (tea.Model, bool) {
	if !m.searchActive {
		return m, false
	}

	m.status = "search results: esc to return to the folder"

	return m, true
}

// renderSearchModal centres the search prompt, the same shape as the filter so the
// interaction vocabulary does not fork.
func (m Model) renderSearchModal(width, height int) string {
	inner := max(1, width-6)

	lines := []string{
		TitleStyle.Render("search"),
		"",
		NormalStyle.Render(TruncateDisplay(m.searchQuery+"_", inner)),
		"",
		MutedStyle.Render(TruncateDisplay("from: subject: text: body: since:YYYY-MM-DD", inner)),
		"",
		MutedStyle.Render("enter:search  esc:cancel"),
	}

	modal := ModalStyle.Render(strings.Join(lines, "\n"))

	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, modal)
}
