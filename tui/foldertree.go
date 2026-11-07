package tui

import (
	"strings"

	"github.com/renderorange/chute/mail"
)

// folderTreeRow is one visible row of the folder pane: which folder it is, how deep
// the tree draws it, and whether it hides subfolders. The expansion set lives on the
// model, keyed by mailbox name; a name that is not in it is collapsed, which is also
// what an absent state file means.
type folderTreeRow struct {
	index       int // into Model.folders
	depth       int
	hasChildren bool
	expanded    bool
}

// folderTree is the visible rows of the listing: every root, and the descendants of
// each expanded folder. It is derived on demand from the listing and the expansion
// set, so no second copy of the tree can go stale.
//
// A parent is the nearest ancestor the listing actually contains. Servers may omit an
// intermediate container, and some omit the parent entirely; those folders become roots
// rather than getting an invented parent row. The depth drawn is the tree depth, not
// the delimiter count, so an orphan still lines up with what is on screen.
func (m Model) folderTree() []folderTreeRow {
	if len(m.folders) == 0 {
		return nil
	}

	byName := make(map[string]int, len(m.folders))
	for i, folder := range m.folders {
		byName[folder.Name] = i
	}

	children := make(map[string][]int, len(m.folders))
	roots := make([]int, 0, 4)

	for i, folder := range m.folders {
		if parent := listedParent(folder, byName); parent != "" {
			children[parent] = append(children[parent], i)
		} else {
			roots = append(roots, i)
		}
	}

	rows := make([]folderTreeRow, 0, len(m.folders))

	var walk func(index, depth int)
	walk = func(index, depth int) {
		kids := children[m.folders[index].Name]
		expanded := m.folderExpanded[m.folders[index].Name]

		rows = append(rows, folderTreeRow{
			index:       index,
			depth:       depth,
			hasChildren: len(kids) > 0,
			expanded:    expanded,
		})

		if !expanded {
			return
		}
		for _, kid := range kids {
			walk(kid, depth+1)
		}
	}

	for _, root := range roots {
		walk(root, 0)
	}

	return rows
}

// listedParent is the nearest ancestor of folder that the listing contains, or "" for
// a root. Walking the whole path is what keeps a folder under a listed grandparent when
// the server skipped the parent in between.
func listedParent(folder mail.Folder, byName map[string]int) string {
	for _, name := range ancestorNames(folder) {
		if _, ok := byName[name]; ok {
			return name
		}
	}

	return ""
}

// ancestorNames lists a folder's ancestors nearest first: Work/Projects/2025 becomes
// ["Work/Projects", "Work"]. A folder with no delimiter has none.
func ancestorNames(folder mail.Folder) []string {
	if folder.Delimiter == 0 {
		return nil
	}

	var names []string

	name := folder.Name
	for {
		idx := strings.LastIndex(name, string(folder.Delimiter))
		if idx < 0 {
			return names
		}
		name = name[:idx]
		names = append(names, name)
	}
}

// selectedTreeRow is the visible row under the cursor.
func (m Model) selectedTreeRow() (folderTreeRow, bool) {
	rows := m.folderTree()
	if m.folderCursor < 0 || m.folderCursor >= len(rows) {
		return folderTreeRow{}, false
	}

	return rows[m.folderCursor], true
}

// treeRowIndex is the visible index of the folder with this name, or -1 when the
// expansion set hides it.
func (m Model) treeRowIndex(name string) int {
	for i, row := range m.folderTree() {
		if m.folders[row.index].Name == name {
			return i
		}
	}

	return -1
}

// folderIndex is the position of the selected folder in the flat listing, which is what
// the folders overlay and the move picker index. It is 0 when nothing is selected.
func (m Model) folderIndex() int {
	row, ok := m.selectedTreeRow()
	if !ok {
		return 0
	}

	return row.index
}

// setFolderExpanded records one folder's state and re-clamps the scroll, because the
// number of visible rows just changed. It does not write the state file: the caller
// decides whether the change was the user's.
func (m *Model) setFolderExpanded(name string, expanded bool) {
	if m.folderExpanded == nil {
		m.folderExpanded = make(map[string]bool)
	}

	if expanded {
		m.folderExpanded[name] = true
	} else {
		delete(m.folderExpanded, name)
	}

	m.clampFolderScroll()
}

// expandTo reveals a folder by expanding its ancestors, for display only. It is what
// makes a remembered or opened nested folder visible without the user expanding its
// path by hand, and it deliberately does not persist: nothing the user chose changed.
func (m *Model) expandTo(name string) {
	if m.folderExpanded == nil {
		m.folderExpanded = make(map[string]bool)
	}

	for _, folder := range m.folders {
		if folder.Name != name {
			continue
		}
		for _, ancestor := range ancestorNames(folder) {
			if _, ok := m.folderByName(ancestor); ok {
				m.folderExpanded[ancestor] = true
			}
		}

		return
	}
}

// folderByName finds a folder in the flat listing.
func (m Model) folderByName(name string) (mail.Folder, bool) {
	for _, folder := range m.folders {
		if folder.Name == name {
			return folder, true
		}
	}

	return mail.Folder{}, false
}

// expandFolderAtCursor reveals the subfolders of the folder under the cursor. It
// reports false when there is nothing to reveal, so the caller can fall through to
// opening the folder: enter expands first, then opens.
func (m *Model) expandFolderAtCursor() bool {
	row, ok := m.selectedTreeRow()
	if !ok || !row.hasChildren || row.expanded {
		return false
	}

	m.setFolderExpanded(m.folders[row.index].Name, true)
	m.status = ""
	m.rememberExpanded()

	return true
}

// collapseFolderAtCursor folds the folder under the cursor. It is the left-arrow half
// of the tree gesture: folding something already folded, or a folder with nothing under
// it, is a no-op rather than a message, because the key is pressed casually.
func (m *Model) collapseFolderAtCursor() {
	row, ok := m.selectedTreeRow()
	if !ok || !row.hasChildren || !row.expanded {
		return
	}

	m.setFolderExpanded(m.folders[row.index].Name, false)
	m.status = ""
	m.rememberExpanded()
}
