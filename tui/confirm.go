package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/emersion/go-imap/v2"
)

// confirmKind says what a confirmation is asking about. The modal text and the action
// it triggers follow from this, rather than from a second boolean per feature.
type confirmKind int

const (
	confirmDelete confirmKind = iota
	confirmDiscardDraft
)

// triageReady reports whether a message panel owns the selection. Without this
// guard, pressing m in the folder list would silently mark mail read. The reading
// pane counts too: it expands the message under the same cursor, and its footer
// advertises d, m and f, so those keys have to work there (#37).
func (m Model) triageReady() bool {
	return m.focus == focusMessages || m.focus == focusReader
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
	m.confirmKind = confirmDelete
	m.pendingDelete = uids
	m.status = ""

	return m, nil
}

// handleConfirmKey applies a keypress while the delete confirmation is open.
func (m Model) handleConfirmKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.keys.resolve(msg.String()) {
	case actionForceQuit:
		return m, tea.Quit

	case actionQuit, actionBack:
		m.confirmOpen = false
		m.pendingDelete = nil
		if m.confirmKind == confirmDiscardDraft {
			m.status = "kept editing"
		} else {
			m.status = "delete cancelled"
		}

	case actionOpen:
		if m.confirmKind == confirmDiscardDraft {
			m.confirmOpen = false
			m.compose = nil
			m.status = "message discarded"

			return m, nil
		}

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
	m.forgetBodies(uids)
	m.rebuildRows()
	m.visualAnchor = noVisualAnchor
	m.openUnseen = countUnread(m.messages)
}

// forgetBodies drops the cached state of messages the server confirmed gone. The
// body of a deleted message is dead weight for the rest of the session, and a
// pending mark-as-read must not outlive the message it was queued for.
func (m *Model) forgetBodies(uids []imap.UID) {
	for _, uid := range uids {
		key := bodyKey{account: m.accountName(), folder: m.openFolder, uid: uid}
		delete(m.bodies, key)
		delete(m.markSeenOnLoad, key)
	}
}

// followDeleted re-opens the message the cursor landed on, so deleting from the
// reading pane moves it to the neighbour instead of leaving an empty selection.
// With nothing left to read, focus goes back to the message list.
func (m Model) followDeleted() (Model, tea.Cmd) {
	if m.focus != focusReader {
		return m, nil
	}

	m.readerScroll = 0

	if len(m.rows) == 0 {
		m.focus = focusMessages

		return m, nil
	}

	next, cmd := m.openSelectedMessage()

	// openSelectedMessage answers with the model it was given on every path; the
	// assertion is what lets this return a Model and keep the caller able to set
	// its status after the load clears it.
	return next.(Model), cmd
}

// renderConfirmModal centres the delete confirmation in the content area. The
// title wears ErrorStyle, not TitleStyle: a destructive confirmation is a
// warning, and a panel heading reads as ordinary chrome. The question and the
// hints keep their normal styles, so only the heading is raised in pitch.
func (m Model) renderConfirmModal(width, height int) string {
	lines := []string{
		ErrorStyle.Render("delete"),
		"",
		fmt.Sprintf("move %s to trash?", countLabel(len(m.pendingDelete), "message")),
		"",
		MutedStyle.Render("enter:delete  esc:cancel"),
	}

	if m.confirmKind == confirmDiscardDraft {
		lines = []string{
			ErrorStyle.Render("discard"),
			"",
			"discard this message? ctrl+s saves it as a draft",
			"",
			MutedStyle.Render("enter:discard  esc:keep editing"),
		}
	}

	modal := ModalStyle.Render(strings.Join(lines, "\n"))

	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, modal)
}
