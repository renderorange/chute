package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/renderorange/chute/mail"
)

// beginMoveFolder opens the destination picker for the folder under the cursor. The
// picker lists parents, not full names, because a move keeps the folder's own name: the
// confirmation shows the full path that results.
func (m Model) beginMoveFolder() (tea.Model, tea.Cmd) {
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

	rows := m.folderParentRows(folder)
	if len(rows) == 0 {
		m.status = "no folder can hold it"

		return m, nil
	}

	m.folderMoveOpen = true
	m.folderMoveName = folder.Name
	m.folderMoveRows = rows
	m.folderMoveMatches = rows
	m.folderMoveCursor = 0
	m.folderMoveFilter = ""
	m.status = ""

	return m, nil
}

// handleFolderMoveKey applies a keypress while the destination picker is open, with the
// same shape as the message-move picker: an action this picker honours still runs, and
// every other printable key belongs to the query.
func (m Model) handleFolderMoveKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.keys.resolve(msg.String()) {
	case actionForceQuit:
		return m, tea.Quit

	case actionQuit, actionBack, actionMoveFolder:
		m.closeFolderMove()

		return m, nil

	case actionOpen:
		return m.chooseFolderParent()

	case actionUp:
		m.folderMoveCursor = max(0, m.folderMoveCursor-1)

	case actionDown:
		m.folderMoveCursor = max(0, min(m.folderMoveCursor+1, len(m.folderMoveMatches)-1))
	}

	switch msg.Type {
	case tea.KeyBackspace:
		if runes := []rune(m.folderMoveFilter); len(runes) > 0 {
			m.folderMoveFilter = string(runes[:len(runes)-1])
			m.applyFolderMoveFilter()
		}

	case tea.KeySpace:
		m.folderMoveFilter += " "
		m.applyFolderMoveFilter()

	case tea.KeyRunes:
		m.folderMoveFilter += string(msg.Runes)
		m.applyFolderMoveFilter()
	}

	return m, nil
}

// applyFolderMoveFilter re-derives the visible destinations and puts the cursor back at
// the top, because the row it pointed at may be gone.
func (m *Model) applyFolderMoveFilter() {
	m.folderMoveMatches = matchFolderParents(m.folderMoveRows, m.folderMoveFilter)
	m.folderMoveCursor = 0
	m.status = ""
}

// closeFolderMove dismisses the picker and forgets the query, so reopening it starts from
// the whole list.
func (m *Model) closeFolderMove() {
	m.folderMoveOpen = false
	m.folderMoveFilter = ""
	m.folderMoveMatches = nil
	m.folderMoveRows = nil
}

// chooseFolderParent turns the chosen parent into a move confirmation. The destination
// path is what the question is about, so it is shown before anything reaches the server.
func (m Model) chooseFolderParent() (tea.Model, tea.Cmd) {
	if len(m.folderMoveMatches) == 0 {
		m.status = "no folders match"

		return m, nil
	}
	if m.folderMoveCursor < 0 || m.folderMoveCursor >= len(m.folderMoveMatches) {
		m.closeFolderMove()

		return m, nil
	}

	target := m.folderMoveMatches[m.folderMoveCursor]
	folder, ok := m.folderByName(m.folderMoveName)

	m.closeFolderMove()
	if !ok {
		m.status = "no folder selected"

		return m, nil
	}

	m.confirmOpen = true
	m.confirmKind = confirmMoveFolder
	m.pendingFolder = folder.Name
	m.pendingFolderParent = target.name
	m.pendingFolderBelow = m.folderDescendants(folder.Name)
	m.status = ""

	return m, nil
}

// renderFolderMoveModal centres the destination picker. The query line appears only once
// something is typed, so the default dialog keeps its rows.
func (m Model) renderFolderMoveModal(width, height int) string {
	inner := max(10, width-14)

	chrome := 6
	lines := []string{TitleStyle.Render(TruncateDisplay("move "+m.folderMoveName+" under", max(1, width-6)))}
	if m.folderMoveFilter != "" {
		chrome++
		lines = append(lines, NormalStyle.Render(TruncateDisplay(m.folderMoveFilter+"_", inner)))
	}

	if len(m.folderMoveMatches) == 0 {
		lines = append(lines, MutedStyle.Render("no folders match"))
	} else {
		rows := max(1, min(len(m.folderMoveMatches), height-chrome))
		start := clamp(m.folderMoveCursor-rows/2, 0, max(0, len(m.folderMoveMatches)-rows))
		end := min(start+rows, len(m.folderMoveMatches))

		for i := start; i < end; i++ {
			lines = append(lines, folderParentRow(m.folderMoveMatches[i], i == m.folderMoveCursor, inner))
		}
	}

	lines = append(lines, "", MutedStyle.Render("enter:choose  esc:close"))

	modal := ModalStyle.Render(strings.Join(lines, "\n"))

	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, modal)
}

// folderParentRow renders one destination: a highlighted row with the full mailbox path,
// so two folders with the same leaf are told apart.
func folderParentRow(parent folderParent, selected bool, width int) string {
	label := TruncateDisplay(parent.label, max(1, width-2))

	if selected {
		return SelectedStyle.Render("> " + label)
	}

	return NormalStyle.Render("  " + label)
}
