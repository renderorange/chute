package tui

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/renderorange/chute/mail"
)

// beginDeleteFolder asks the server how much mail is in the folder before it asks the
// user to confirm. The refusal and the confirmation are separate on purpose: confirming
// an action that is certain to fail is a keystroke spent for nothing, and the count is
// what tells the user what to do about it.
func (m Model) beginDeleteFolder() (tea.Model, tea.Cmd) {
	if !m.folderActionsReady() {
		return m, nil
	}
	if len(m.cfg.Accounts) == 0 {
		return m, nil
	}

	folder, ok := m.folderActionTarget()
	if !ok {
		m.status = "no folder selected"

		return m, nil
	}
	if reason := mail.FolderConflict(m.cfg.Accounts[m.account], folder); reason != "" {
		m.status = reason

		return m, nil
	}

	// The subfolder count is in the listing, so it needs no round trip: refusing here
	// keeps the confirmation for deletes that can actually run.
	if below := m.folderDescendants(folder.Name); below > 0 {
		m.status = fmt.Sprintf("%s has %s below it; delete those first", folder.Name, countLabel(below, "folder"))

		return m, nil
	}
	if m.folderMessages == nil {
		return m, nil
	}

	acct := m.cfg.Accounts[m.account]
	name := folder.Name
	count := m.folderMessages
	m.status = "checking " + name + "..."

	return m, func() tea.Msg {
		messages, err := count(acct, name)

		return folderCheckedMsg{account: acct.Name, name: name, count: messages, err: err}
	}
}
