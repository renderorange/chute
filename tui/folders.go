package tui

import (
	"strings"

	"github.com/renderorange/chute/mail"
)

// folderRows is how many folder lines fit in the folder panel right now.
func (m Model) folderRows() int {
	if m.width == 0 || m.height == 0 {
		return 1
	}

	layout := ComputeLayout(m.width, m.height)
	if layout.Folders.Empty() {
		return layout.Reader.Height - 1
	}

	rows := layout.Folders.Height - 1
	if rows < 1 {
		return 1
	}

	return rows
}

// moveFolderCursor moves the selection and keeps it inside the scroll window.
func (m *Model) moveFolderCursor(delta int) {
	if len(m.folders) == 0 {
		return
	}

	m.folderCursor = clamp(m.folderCursor+delta, 0, len(m.folders)-1)

	rows := m.folderRows()
	if m.folderCursor < m.folderScroll {
		m.folderScroll = m.folderCursor
	}
	if m.folderCursor >= m.folderScroll+rows {
		m.folderScroll = m.folderCursor - rows + 1
	}
}

// clampFolderScroll keeps the stored scroll offset valid after a reload or resize.
func (m *Model) clampFolderScroll() {
	rows := m.folderRows()
	maxScroll := max(0, len(m.folders)-rows)

	m.folderScroll = clamp(m.folderScroll, 0, maxScroll)
	m.folderCursor = clamp(m.folderCursor, 0, max(0, len(m.folders)-1))

	if m.folderCursor < m.folderScroll {
		m.folderScroll = m.folderCursor
	}
	if m.folderCursor >= m.folderScroll+rows {
		m.folderScroll = m.folderCursor - rows + 1
	}
}

// folderLines renders the visible slice of the folder list.
func (m Model) folderLines(rows int) []string {
	if rows < 1 {
		return nil
	}

	switch m.state {
	case stateConnecting:
		return []string{MutedStyle.Render("connecting…")}
	case stateFailed:
		if len(m.folders) == 0 {
			return []string{MutedStyle.Render("no folders")}
		}
	}

	if len(m.folders) == 0 {
		return []string{MutedStyle.Render("no folders")}
	}

	start := clamp(m.folderScroll, 0, max(0, len(m.folders)-rows))
	end := min(start+rows, len(m.folders))

	lines := make([]string, 0, end-start)
	for i := start; i < end; i++ {
		lines = append(lines, m.folderLine(i))
	}

	return lines
}

// folderLine renders one folder, indented by depth and marked when selected.
func (m Model) folderLine(index int) string {
	folder := m.folders[index]
	label := strings.Repeat("  ", folder.Depth()) + folder.Leaf()

	if !folder.Selectable() {
		return MutedStyle.Render(label)
	}
	if index == m.folderCursor {
		return SelectedStyle.Render("▸ " + label)
	}

	return NormalStyle.Render("  " + label)
}

// selectedFolder is the folder under the cursor, if there is one.
func (m Model) selectedFolder() (mail.Folder, bool) {
	if m.folderCursor < 0 || m.folderCursor >= len(m.folders) {
		return mail.Folder{}, false
	}

	return m.folders[m.folderCursor], true
}
