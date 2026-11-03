package tui

import (
	"errors"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/renderorange/chute/mail"
)

// beginArchive files the selection under the account's archive mailbox.
func (m Model) beginArchive() (tea.Model, tea.Cmd) {
	return m.moveSelectionTo(mail.RoleArchive)
}

// beginJunk files the selection under the account's junk mailbox.
func (m Model) beginJunk() (tea.Model, tea.Cmd) {
	return m.moveSelectionTo(mail.RoleJunk)
}

// moveSelectionTo is the shared path behind archive and junk: the same guard and the
// same visual range as delete, and no confirmation, because a move is reversible with
// the same keys (GH-22 ruling).
func (m Model) moveSelectionTo(role mail.Role) (tea.Model, tea.Cmd) {
	if !m.triageReady() {
		return m, nil
	}
	if next, blocked := m.resultsOnly(); blocked {
		return next, nil
	}

	uids := m.actionUIDs()
	if len(uids) == 0 {
		m.status = "no message selected"
		return m, nil
	}

	if m.moveToRole == nil || m.openFolder == "" || len(m.cfg.Accounts) == 0 {
		return m, nil
	}

	acct := m.cfg.Accounts[m.account]
	folder := m.openFolder
	move := m.moveToRole
	m.status = ""

	return m, func() tea.Msg {
		dest, err := move(acct, folder, role, uids)

		return moveMsg{account: acct.Name, folder: folder, role: role, dest: dest, uids: uids, err: err}
	}
}

// moveStatus turns the typed refusals into short statuses the status bar can show.
// Anything else is shown as the server said it.
func moveStatus(msg moveMsg) string {
	switch {
	case errors.Is(msg.err, mail.ErrNoRoleFolder):
		if msg.role == mail.RoleArchive {
			return "no archive folder configured"
		}

		return "no junk folder configured"

	case errors.Is(msg.err, mail.ErrAlreadyInRoleFolder):
		if msg.dest != "" {
			return "already in " + msg.dest
		}

		return "already there"
	}

	return statusText(msg.err)
}
