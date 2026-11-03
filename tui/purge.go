package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/emersion/go-imap/v2"
	"github.com/renderorange/chute/mail"
)

// purgeMsg reports a permanent delete of named UIDs inside Trash.
type purgeMsg struct {
	account string
	folder  string
	uids    []imap.UID
	err     error
}

// emptyTrashMsg reports an explicit empty of the trash mailbox.
type emptyTrashMsg struct {
	account string
	folder  string
	uids    []imap.UID
	err     error
}

// trashFolderName resolves the mailbox serving the trash role for the active account,
// including the [accounts.imap] trash override.
func (m Model) trashFolderName() (string, bool) {
	if len(m.cfg.Accounts) == 0 {
		return "", false
	}

	folder, ok := mail.ResolveFolder(m.folders, mail.RoleTrash, m.cfg.Accounts[m.account].IMAP.Trash)
	if !ok {
		return "", false
	}

	return folder.Name, true
}

// inTrash reports whether the open folder is the trash mailbox. Delete means something
// different there: a move to Trash inside Trash is a no-op, so d becomes permanent.
func (m Model) inTrash() bool {
	name, ok := m.trashFolderName()

	return ok && strings.EqualFold(name, m.openFolder)
}

// beginPurge asks the harder question for a permanent delete. The selection is the same
// one delete and the moves use, so visual ranges work here too.
func (m Model) beginPurge() (tea.Model, tea.Cmd) {
	if !m.triageReady() {
		return m, nil
	}

	uids := m.actionUIDs()
	if len(uids) == 0 {
		m.status = "no message selected"
		return m, nil
	}

	m.confirmOpen = true
	m.confirmKind = confirmPurge
	m.pendingDelete = uids
	m.pendingEmpty = 0
	m.status = ""

	return m, nil
}

// beginEmptyTrash asks before deleting every message in the trash mailbox. It is the only
// action allowed to run a folder-wide EXPUNGE, and only because the user asked for the
// whole folder.
func (m Model) beginEmptyTrash() (tea.Model, tea.Cmd) {
	if !m.triageReady() {
		return m, nil
	}
	if !m.inTrash() {
		m.status = "not in Trash"
		return m, nil
	}
	if len(m.messages) == 0 {
		m.status = "Trash is empty"
		return m, nil
	}

	m.confirmOpen = true
	m.confirmKind = confirmEmptyTrash
	m.pendingDelete = nil
	m.pendingEmpty = len(m.messages)
	m.status = ""

	return m, nil
}

// purgeCmd permanently deletes the selection in the background.
func (m Model) purgeCmd(uids []imap.UID) tea.Cmd {
	if m.purgeMessages == nil || m.openFolder == "" || len(m.cfg.Accounts) == 0 {
		return nil
	}

	acct := m.cfg.Accounts[m.account]
	folder := m.openFolder
	purge := m.purgeMessages

	return func() tea.Msg {
		err := purge(acct, folder, uids)

		return purgeMsg{account: acct.Name, folder: folder, uids: uids, err: err}
	}
}

// emptyTrashCmd empties the trash mailbox in the background. It captures the UIDs known
// now so the success path can drop the rows without a refetch.
func (m Model) emptyTrashCmd() tea.Cmd {
	if m.emptyTrash == nil || m.openFolder == "" || len(m.cfg.Accounts) == 0 {
		return nil
	}

	uids := make([]imap.UID, 0, len(m.messages))
	for _, msg := range m.messages {
		uids = append(uids, msg.UID)
	}

	acct := m.cfg.Accounts[m.account]
	folder := m.openFolder
	empty := m.emptyTrash

	return func() tea.Msg {
		err := empty(acct)

		return emptyTrashMsg{account: acct.Name, folder: folder, uids: uids, err: err}
	}
}
