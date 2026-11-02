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

	layout := m.layout()
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
func (m Model) folderLines(rows, width int) []string {
	if rows < 1 {
		return nil
	}

	switch m.state {
	case stateConnecting:
		return []string{MutedStyle.Render("connecting...")}
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
		lines = append(lines, m.folderLine(i, width))
	}

	return lines
}

// folderLine renders one folder, indented by depth and marked when selected.
func (m Model) folderLine(index, width int) string {
	return folderRow(m.folders[index], index == m.folderCursor, width)
}

// folderRow renders one folder row: indent, leaf, marker and styling. The label
// is truncated so a long name never wraps onto a second line.
func folderRow(folder mail.Folder, selected bool, width int) string {
	label := strings.Repeat("  ", folder.Depth()) + folder.Leaf()
	label = Truncate(label, max(1, width-2))

	if !folder.Selectable() {
		return MutedStyle.Render("  " + label)
	}
	if selected {
		return SelectedStyle.Render("> " + label)
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

// moveFolderCursorTo puts the folder cursor on folder, if the listing still has it.
func (m *Model) moveFolderCursorTo(folder mail.Folder) {
	for i := range m.folders {
		if m.folders[i].Name == folder.Name {
			m.folderCursor = i

			return
		}
	}
}

// startFolder resolves the account's start folder against the loaded list.
func (m Model) startFolder() (mail.Folder, bool) {
	if m.account < 0 || m.account >= len(m.cfg.Accounts) {
		return mail.Folder{}, false
	}

	name := m.cfg.Accounts[m.account].StartFolderName()
	if name == "" {
		return mail.Folder{}, false
	}

	inbox := strings.EqualFold(name, "INBOX")
	for _, folder := range m.folders {
		if !folder.Selectable() {
			continue
		}
		if inbox && folder.Role() == mail.RoleInbox {
			return folder, true
		}
		if strings.EqualFold(folder.Name, name) {
			return folder, true
		}
	}

	return mail.Folder{}, false
}
