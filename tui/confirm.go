package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/emersion/go-imap/v2"
)

// triageReady reports whether the message list owns the selection. Without this
// guard, pressing m in the folder list would silently mark mail read.
func (m Model) triageReady() bool {
	return m.focus == focusMessages
}

// beginDelete asks before moving the selection to Trash. Nothing reaches the
// server until the confirmation is answered.
func (m Model) beginDelete() (tea.Model, tea.Cmd) {
	if !m.triageReady() {
		return m, nil
	}

	uids := m.actionUIDs()
	if len(uids) == 0 {
		m.status = "no message selected"
		return m, nil
	}

	m.confirmOpen = true
	m.pendingDelete = uids
	m.status = ""

	return m, nil
}

// handleConfirmKey applies a keypress while the delete confirmation is open.
func (m Model) handleConfirmKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.keys.resolve(msg.String()) {
	case actionQuit:
		return m, tea.Quit

	case actionBack:
		m.confirmOpen = false
		m.pendingDelete = nil
		m.status = "delete cancelled"

	case actionOpen:
		uids := m.pendingDelete
		m.confirmOpen = false
		m.pendingDelete = nil

		if len(uids) == 0 {
			return m, nil
		}

		return m, m.deleteCmd(uids)
	}

	return m, nil
}

// deleteCmd moves the selection to the account's trash mailbox in the
// background. The mail layer resolves the folder and picks the protocol.
func (m Model) deleteCmd(uids []imap.UID) tea.Cmd {
	if m.deleteMessages == nil || m.openFolder == "" || len(m.cfg.Accounts) == 0 {
		return nil
	}

	acct := m.cfg.Accounts[m.account]
	folder := m.openFolder
	remove := m.deleteMessages

	return func() tea.Msg {
		err := remove(acct, folder, uids)

		return deleteMsg{account: acct.Name, folder: folder, uids: uids, err: err}
	}
}

// removeDeleted drops the rows the server confirmed and leaves the cursor on a
// neighbour. A folder refetch after every delete would be far more expensive.
func (m *Model) removeDeleted(uids []imap.UID) {
	gone := make(map[imap.UID]bool, len(uids))
	for _, uid := range uids {
		gone[uid] = true
	}

	kept := m.messages[:0]
	for _, msg := range m.messages {
		if !gone[msg.UID] {
			kept = append(kept, msg)
		}
	}
	m.messages = kept
	m.messageCursor = clamp(m.messageCursor, 0, max(0, len(m.messages)-1))
	m.clampMessageScroll()
	m.visualAnchor = noVisualAnchor
}

// renderConfirmModal centres the delete confirmation in the content area.
func (m Model) renderConfirmModal(width, height int) string {
	lines := []string{
		TitleStyle.Render("delete"),
		"",
		fmt.Sprintf("move %s to trash?", countLabel(len(m.pendingDelete), "message")),
		"",
		MutedStyle.Render("enter:delete  esc:cancel"),
	}

	modal := ModalStyle.Render(strings.Join(lines, "\n"))

	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, modal)
}
