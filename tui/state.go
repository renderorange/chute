package tui

import (
	"strings"

	"github.com/renderorange/chute/config"
	"github.com/renderorange/chute/mail"
)

// WithSelection installs the selection remembered from the previous run, and the
// writer that persists it. Later selections are written as they happen, so a killed
// terminal does not lose them.
//
// A remembered account that is no longer in the config is ignored, which leaves the
// configured default in place: the config is the authority, and a removed account is
// not an error. A nil writer keeps the model inert, which is what tests rely on.
func (m Model) WithSelection(sel config.State, save func(config.State) error) Model {
	m.selection = sel
	m.saveState = save

	if index, ok := m.accountIndex(sel.Account); ok {
		m.account = index
	}

	return m
}

// accountIndex finds an account by name. An empty or unknown name reports false.
func (m Model) accountIndex(name string) (int, bool) {
	if name == "" {
		return 0, false
	}

	for i := range m.cfg.Accounts {
		if m.cfg.Accounts[i].Name == name {
			return i, true
		}
	}

	return 0, false
}

// rememberedFolder resolves the folder this account had open last time, if the loaded
// listing still has it. A folder the user renamed or hid with hide_folders is simply
// not found, which is what makes the start folder take over silently.
func (m Model) rememberedFolder() (mail.Folder, bool) {
	name := strings.TrimSpace(m.selection.Folders[m.accountName()])
	if name == "" {
		return mail.Folder{}, false
	}

	for _, folder := range m.folders {
		if folder.Selectable() && folder.Name == name {
			return folder, true
		}
	}

	return mail.Folder{}, false
}

// rememberSelection records the account now active and the folder it has open, then
// writes them. A write failure is silent: a read-only home or a full disk must not
// interrupt the user's action or produce a message on every folder change.
//
// An empty open folder keeps the account's previous entry, so switching account
// without opening a folder does not forget where that account was.
func (m *Model) rememberSelection() {
	m.selection.Account = m.accountName()

	if m.selection.Folders == nil {
		m.selection.Folders = make(map[string]string)
	}
	if m.openFolder != "" {
		m.selection.Folders[m.accountName()] = m.openFolder
	}

	if m.saveState != nil {
		_ = m.saveState(m.selection)
	}
}
