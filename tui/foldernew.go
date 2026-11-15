package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// beginNewFolder opens the create prompt for the folder under the cursor. The new folder
// is created under that folder, which is why the prompt shows the resolved path: the
// base is a fact about where the cursor was, and the user should see it before
// committing.
func (m Model) beginNewFolder() (tea.Model, tea.Cmd) {
	if !m.folderActionsReady() {
		return m, nil
	}

	folder, ok := m.folderActionTarget()
	if !ok {
		m.status = "no folder selected"

		return m, nil
	}

	m.newFolderOpen = true
	m.folderBase = folder
	m.folderName = ""
	m.status = ""

	return m, nil
}

// handleNewFolderKey edits the name. Every printable key belongs to it, which is why this
// prompt reads the key type rather than the binding table: a folder may be called
// "Quick notes", and a keymap lookup would treat the q as quit. The filter prompt makes
// the same choice for the same reason.
func (m Model) handleNewFolderKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyCtrlC:
		return m, tea.Quit

	case tea.KeyEsc:
		m.newFolderOpen = false
		m.folderName = ""
		m.status = "new folder cancelled"

		return m, nil

	case tea.KeyEnter:
		return m.createFolder()

	case tea.KeyBackspace:
		if runes := []rune(m.folderName); len(runes) > 0 {
			m.folderName = string(runes[:len(runes)-1])
		}

	case tea.KeySpace:
		m.folderName += " "

	case tea.KeyRunes:
		m.folderName += string(msg.Runes)
	}

	return m, nil
}

// createFolder starts the create in the background. Nothing reaches the server while the
// name is still illegal or already taken, so the status explains the problem instead.
func (m Model) createFolder() (tea.Model, tea.Cmd) {
	if problem := m.folderNewProblem(); problem != "" {
		m.status = problem

		return m, nil
	}

	name := m.folderNewTarget()
	if name == "" {
		m.status = "a folder needs a name"

		return m, nil
	}
	if m.newFolder == nil || len(m.cfg.Accounts) == 0 {
		return m, nil
	}

	acct := m.cfg.Accounts[m.account]
	create := m.newFolder

	m.newFolderOpen = false
	m.folderName = ""
	m.status = ""

	return m, func() tea.Msg {
		folders, err := create(acct, name)

		return folderCreatedMsg{account: acct.Name, name: name, folders: folders, err: err}
	}
}

// renderNewFolderModal centres the create prompt. The second line is the full name the
// server will be asked for, or the reason the typed name cannot be used, which makes the
// base folder and the nesting visible while there is still time to change them.
func (m Model) renderNewFolderModal(width, height int) string {
	inner := max(1, width-6)

	target := m.folderNewTarget()
	problem := m.folderNewProblem()

	preview := MutedStyle.Render(TruncateDisplay("in "+m.folderBase.Name, inner))
	if problem != "" {
		preview = ErrorStyle.Render(TruncateDisplay(problem, inner))
	} else if target != "" {
		preview = NormalStyle.Render(TruncateDisplay("will create: "+target, inner))
	}

	lines := []string{
		TitleStyle.Render("new folder"),
		"",
		NormalStyle.Render(TruncateDisplay(m.folderName+"_", inner)),
		preview,
		"",
		MutedStyle.Render("enter:create  esc:cancel"),
	}

	modal := ModalStyle.Render(strings.Join(lines, "\n"))

	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, modal)
}
