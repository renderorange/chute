package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// openFolders focuses the folders pane when it is drawn, and opens the overlay
// when the current layout does not draw it.
func (m Model) openFolders() (tea.Model, tea.Cmd) {
	if len(m.folders) == 0 {
		m.status = "no folders"
		return m, nil
	}

	if ComputeLayout(m.width, m.height).Mode != ModeTwo {
		m.focus = focusFolders
		m.status = ""
		return m, nil
	}

	m.foldersOpen = true
	m.folderOverlayCursor = m.folderCursor
	m.status = ""

	return m, nil
}

// handleFoldersKey applies a keypress while the folder picker is open.
func (m Model) handleFoldersKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.keys.resolve(msg.String()) {
	case actionQuit:
		return m, tea.Quit

	case actionBack, actionFoldersOverlay:
		m.foldersOpen = false

	case actionUp:
		m.folderOverlayCursor = max(0, m.folderOverlayCursor-1)

	case actionDown:
		m.folderOverlayCursor = min(m.folderOverlayCursor+1, len(m.folders)-1)

	case actionOpen:
		m.folderCursor = m.folderOverlayCursor
		m.foldersOpen = false
		return m.openSelectedFolder()
	}

	return m, nil
}

// renderFoldersOverlay centres the folder picker in the content area.
func (m Model) renderFoldersOverlay(width, height int) string {
	if len(m.folders) == 0 {
		return ""
	}

	rows := max(1, min(len(m.folders), height-6))
	start := clamp(m.folderOverlayCursor-rows/2, 0, max(0, len(m.folders)-rows))
	end := min(start+rows, len(m.folders))

	lines := []string{TitleStyle.Render("folders")}
	for i := start; i < end; i++ {
		lines = append(lines, folderRow(m.folders[i], i == m.folderOverlayCursor, max(10, width-14)))
	}
	lines = append(lines, "", MutedStyle.Render("enter:open  esc:close"))

	modal := ModalStyle.Render(strings.Join(lines, "\n"))

	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, modal)
}
