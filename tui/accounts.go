package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/emersion/go-imap/v2"
	"github.com/renderorange/chute/mail"
)

// openAccounts opens the account selector and starts one unread count per
// account. The counts run as a batch, so an unreachable server delays its own row
// rather than the whole dialog.
func (m Model) openAccounts() (tea.Model, tea.Cmd) {
	if len(m.cfg.Accounts) == 0 {
		m.status = "no accounts"
		return m, nil
	}

	m.accountsOpen = true
	m.accountCursor = m.account
	m.status = ""
	m.unseen = make(map[string]uint32, len(m.cfg.Accounts))
	m.unseenErrs = make(map[string]error)

	load := m.countUnseen
	if load == nil {
		return m, nil
	}

	cmds := make([]tea.Cmd, 0, len(m.cfg.Accounts))
	for i := range m.cfg.Accounts {
		acct := m.cfg.Accounts[i]
		cmds = append(cmds, func() tea.Msg {
			count, err := load(acct, "INBOX")

			return unseenMsg{account: acct.Name, count: count, err: err}
		})
	}

	return m, tea.Batch(cmds...)
}

// handleAccountsKey applies a keypress while the account selector is open.
func (m Model) handleAccountsKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.keys.resolve(msg.String()) {
	case actionForceQuit:
		return m, tea.Quit

	case actionQuit, actionBack, actionAccount:
		m.accountsOpen = false

	case actionUp:
		m.accountCursor = max(0, m.accountCursor-1)

	case actionDown:
		m.accountCursor = min(m.accountCursor+1, len(m.cfg.Accounts)-1)

	case actionOpen:
		return m.selectAccount(m.accountCursor)
	}

	return m, nil
}

// selectAccount makes index the active account and reloads its folders.
//
// Nothing about the previous account survives except its open session: the folder
// list, the messages, the body cache and the mark-seen set all describe one
// mailbox tree, and showing them under another account's name would be a lie. The
// started flag is re-armed so the new account opens its start folder.
func (m Model) selectAccount(index int) (tea.Model, tea.Cmd) {
	if index < 0 || index >= len(m.cfg.Accounts) {
		return m, nil
	}

	m.accountsOpen = false
	m.accountCursor = index

	if index == m.account {
		return m, nil
	}

	m.account = index
	m.state = stateConnecting
	m.status = ""

	m.folders = nil
	m.folderCursor = 0
	m.folderScroll = 0

	m.openFolder = ""
	m.messages = nil
	m.rows = nil
	m.filter = ""
	m.filterOpen = false
	m.messageCursor = 0
	m.messageScroll = 0
	m.msgLoading = false
	m.openUnseen = 0

	m.bodies = make(map[bodyKey]*mail.Body)
	m.markSeenOnLoad = make(map[bodyKey]bool)
	m.loadingBody = bodyKey{}
	m.readerScroll = 0
	m.visualAnchor = noVisualAnchor
	m.collapsed = make(map[imap.UID]bool)

	m.attachOpen = false
	m.attachCursor = 0
	m.foldersOpen = false
	m.folderOverlayCursor = 0

	m.started = false

	// The switch is written now, not at exit: a killed terminal must not lose it. The
	// new account's own folder entry is left alone, so it reopens where it was.
	m.rememberSelection()

	m.focus = focusFolders
	m.clampFocus()

	return m, m.reloadCmd()
}

// applyUnseen records one account's count or its failure. A result that arrives
// after the selector closed is dropped; reopening asks again.
func (m *Model) applyUnseen(msg unseenMsg) {
	if !m.accountsOpen {
		return
	}
	if m.unseen == nil {
		m.unseen = make(map[string]uint32)
	}
	if m.unseenErrs == nil {
		m.unseenErrs = make(map[string]error)
	}

	if msg.err != nil {
		m.unseenErrs[msg.account] = msg.err
		delete(m.unseen, msg.account)

		return
	}

	delete(m.unseenErrs, msg.account)
	m.unseen[msg.account] = msg.count
}

// renderAccountsModal centres the account selector in the content area.
func (m Model) renderAccountsModal(width, height int) string {
	if len(m.cfg.Accounts) == 0 {
		return ""
	}

	rows := max(1, min(len(m.cfg.Accounts), height-6))
	start := clamp(m.accountCursor-rows/2, 0, max(0, len(m.cfg.Accounts)-rows))
	end := min(start+rows, len(m.cfg.Accounts))
	rowWidth := max(10, width-14)

	lines := []string{TitleStyle.Render("accounts")}
	for i := start; i < end; i++ {
		lines = append(lines, m.accountRow(i, rowWidth))
	}
	lines = append(lines, "", MutedStyle.Render("enter:switch  esc:close"))

	modal := ModalStyle.Render(strings.Join(lines, "\n"))

	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, modal)
}

// accountRow renders one account with its unread count, or the reason the count
// could not be read. A failed count does not make the account unselectable.
func (m Model) accountRow(index, width int) string {
	acct := m.cfg.Accounts[index]

	detail := "counting..."
	if err := m.unseenErrs[acct.Name]; err != nil {
		detail = err.Error()
	} else if count, ok := m.unseen[acct.Name]; ok {
		detail = fmt.Sprintf("%d unread", count)
	}

	line := Truncate(acct.Name+"  "+detail, max(1, width))
	if index == m.accountCursor {
		return SelectedStyle.Render("> " + line)
	}

	return NormalStyle.Render("  " + line)
}
