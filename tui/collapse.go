package tui

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/emersion/go-imap/v2"
)

// collapseRows folds every collapsed conversation into its root row. It returns the
// rows to draw and a map from each hidden UID to the root that hides it, so the caller
// can keep the cursor somewhere real after a rebuild.
//
// Collapsing is suspended while a filter is active: a query that matched a reply inside
// a collapsed thread must never be hidden behind a count. Clearing the filter restores
// the collapse.
func collapseRows(rows []Row, collapsed map[imap.UID]bool, active bool) ([]Row, map[imap.UID]imap.UID) {
	if !active || len(collapsed) == 0 || len(rows) == 0 {
		return rows, nil
	}

	folded := make([]Row, 0, len(rows))
	hiddenBy := make(map[imap.UID]imap.UID)

	for i := 0; i < len(rows); {
		row := rows[i]
		if row.Depth != 0 || !collapsed[row.Message.UID] {
			folded = append(folded, row)
			i++
			continue
		}

		end := i
		for end < len(rows) && rows[end].Thread == row.Thread {
			end++
		}

		root := row
		root.Hidden = end - i - 1
		for j := i + 1; j < end; j++ {
			if !rows[j].Message.Seen() {
				root.HiddenUnread = true
			}
			hiddenBy[rows[j].Message.UID] = row.Message.UID
		}

		folded = append(folded, root)
		i = end
	}

	return folded, hiddenBy
}

// threadRootUID returns the UID of the root of the conversation the cursor is in. Rows
// are ordered root first within a block, so the first row with the same thread is it.
func (m Model) threadRootUID(index int) (imap.UID, bool) {
	if index < 0 || index >= len(m.rows) {
		return 0, false
	}

	thread := m.rows[index].Thread
	for _, row := range m.rows {
		if row.Thread == thread {
			return row.Message.UID, true
		}
	}

	return 0, false
}

// threadSize is how many rows the cursor's conversation occupies.
func (m Model) threadSize(index int) int {
	if index < 0 || index >= len(m.rows) {
		return 0
	}

	thread := m.rows[index].Thread

	size := 0
	for _, row := range m.rows {
		if row.Thread == thread {
			size++
		}
	}

	return size
}

// toggleCollapse folds or unfolds the conversation under the cursor and leaves the
// cursor on its root, so the same key toggles the same thread back.
func (m Model) toggleCollapse() (tea.Model, tea.Cmd) {
	if !m.triageReady() || len(m.rows) == 0 {
		return m, nil
	}
	if m.filter != "" {
		m.status = "clear the filter to collapse"
		return m, nil
	}

	root, ok := m.threadRootUID(m.messageCursor)
	if !ok {
		return m, nil
	}

	// While collapsed, only the root is drawn, so the visible count is short by the
	// replies it hides; use the fold's own record to know the real size.
	collapsedNow := m.collapsed[root]
	size := m.threadSize(m.messageCursor)
	if collapsedNow {
		size += m.rows[m.messageCursor].Hidden
	}
	if size < 2 {
		m.status = "thread has one message"
		return m, nil
	}

	if m.collapsed == nil {
		m.collapsed = make(map[imap.UID]bool)
	}

	collapsing := !collapsedNow
	if collapsing {
		m.collapsed[root] = true
	} else {
		delete(m.collapsed, root)
	}

	m.visualAnchor = noVisualAnchor
	m.rebuildRows()

	for i, row := range m.rows {
		if row.Message.UID == root {
			m.messageCursor = i
			break
		}
	}
	m.clampMessageScroll()

	if collapsing {
		m.status = "collapsed thread"
	} else {
		m.status = "expanded thread"
	}

	return m, nil
}
