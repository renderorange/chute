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

// moveFolderCursor moves the selection and keeps it inside the scroll window. The
// cursor indexes the visible tree, not the listing: a hidden subfolder cannot be
// selected, and a collapsed parent keeps the cursor where the user left it.
func (m *Model) moveFolderCursor(delta int) {
	rows := m.folderTree()
	if len(rows) == 0 {
		return
	}

	m.folderCursor = clamp(m.folderCursor+delta, 0, len(rows)-1)

	visible := m.folderRows()
	if m.folderCursor < m.folderScroll {
		m.folderScroll = m.folderCursor
	}
	if m.folderCursor >= m.folderScroll+visible {
		m.folderScroll = m.folderCursor - visible + 1
	}
}

// clampFolderScroll keeps the stored scroll offset valid after a reload, a resize or a
// fold that changed how many rows are drawn.
func (m *Model) clampFolderScroll() {
	rows := len(m.folderTree())
	visible := m.folderRows()
	maxScroll := max(0, rows-visible)

	m.folderScroll = clamp(m.folderScroll, 0, maxScroll)
	m.folderCursor = clamp(m.folderCursor, 0, max(0, rows-1))

	if m.folderCursor < m.folderScroll {
		m.folderScroll = m.folderCursor
	}
	if m.folderCursor >= m.folderScroll+visible {
		m.folderScroll = m.folderCursor - visible + 1
	}
}

// folderLines renders the visible slice of the folder tree.
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

	tree := m.folderTree()
	start := clamp(m.folderScroll, 0, max(0, len(tree)-rows))
	end := min(start+rows, len(tree))

	lines := make([]string, 0, end-start)
	for i := start; i < end; i++ {
		lines = append(lines, m.folderTreeLine(tree[i], i == m.folderCursor, width))
	}

	return lines
}

// folderTreeLine renders one row of the folder tree in the shape imap-notes uses: the
// +/- marker leads the row and the cursor is the bold row, with no separate glyph, so
// the marker is never displaced by the selection. A folder with no subfolders shows a
// blank marker rather than a sign that promises nothing.
func (m Model) folderTreeLine(row folderTreeRow, selected bool, width int) string {
	folder := m.folders[row.index]

	marker := "  "
	if row.hasChildren {
		if row.expanded {
			marker = "- "
		} else {
			marker = "+ "
		}
	}

	label := strings.Repeat("  ", row.depth) + folder.Leaf()
	label = Truncate(label, max(1, width-2))
	line := marker + label

	switch {
	case selected:
		return SelectedStyle.Render(line)
	case !folder.Selectable():
		return MutedStyle.Render(line)
	}

	return NormalStyle.Render(line)
}

// folderRow renders one folder row for a flat list: the folders overlay and the move
// picker show every mailbox, including the ones the tree currently hides.
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
	row, ok := m.selectedTreeRow()
	if !ok {
		return mail.Folder{}, false
	}

	return m.folders[row.index], true
}

// moveFolderCursorTo puts the folder cursor on folder and reveals its path, if the
// listing still has it.
func (m *Model) moveFolderCursorTo(folder mail.Folder) {
	m.expandTo(folder.Name)

	if index := m.treeRowIndex(folder.Name); index >= 0 {
		m.folderCursor = index
		m.clampFolderScroll()
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
