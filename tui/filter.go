package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/sahilm/fuzzy"
)

// filterFields is how many strings are searched per row: From, Subject and To. A
// fuzzy hit is mapped back to its row by dividing its field index by this.
const filterFields = 3

// matchingRows reports which rows match the query. The match is case-insensitive
// and fuzzy, the same rules imap-notes uses: "ivm" finds "Invoice March". The
// answer is membership only, because a filter hides rows and never reorders them.
func matchingRows(rows []Row, query string) map[int]bool {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil
	}

	fields := make([]string, 0, len(rows)*filterFields)
	for _, row := range rows {
		msg := row.Message
		fields = append(fields, msg.From(), msg.Subject(), strings.Join(msg.ToAddresses(), " "))
	}

	matched := make(map[int]bool, len(rows))
	for _, hit := range fuzzy.FindNoSort(query, fields) {
		matched[hit.Index/filterFields] = true
	}

	return matched
}

// filterRows narrows the visible list to the matching messages plus the ancestors
// they need. A matching reply whose parent does not match still shows the parent:
// without it the reply would read as a conversation root and its indent would lie.
func (m Model) filterRows(rows []Row) []Row {
	matched := matchingRows(rows, m.filter)
	if matched == nil {
		return rows
	}

	keep := make([]bool, len(rows))
	for i := range rows {
		if !matched[i] {
			continue
		}
		keep[i] = true
		keepAncestors(rows, keep, i)
	}

	filtered := make([]Row, 0, len(keep))
	for i, row := range rows {
		if keep[i] {
			filtered = append(filtered, row)
		}
	}

	return filtered
}

// keepAncestors marks the rows above a kept row that give it its indent: the
// nearest earlier row of the same conversation with a smaller depth, then that
// row's own ancestor, and so on up to the root.
func keepAncestors(rows []Row, keep []bool, index int) {
	depth, thread := rows[index].Depth, rows[index].Thread

	for j := index - 1; j >= 0 && rows[j].Thread == thread; j-- {
		if rows[j].Depth >= depth {
			continue
		}

		keep[j] = true
		depth = rows[j].Depth
		if depth == 0 {
			return
		}
	}
}

// visibleRows derives the list for the current query.
func (m Model) visibleRows() []Row {
	return m.filterRows(BuildRows(m.messages))
}

// applyFilter re-derives the rows after the query changed. The cursor stays on the
// same message when the filter keeps it and moves to the first match when it does
// not, so typing always leaves the selection somewhere visible.
func (m *Model) applyFilter() {
	keep, hasCursor := m.selectedMessage()

	m.rows = m.visibleRows()
	m.messageScroll = 0
	m.readerScroll = 0
	m.visualAnchor = noVisualAnchor
	m.messageCursor = 0

	if hasCursor {
		for i, row := range m.rows {
			if row.Message.UID == keep.UID {
				m.messageCursor = i
				break
			}
		}
	}

	m.clampMessageScroll()
}

// openFilter opens the prompt. The rows narrow as the query grows; esc clears the
// query and restores every row.
func (m Model) openFilter() (tea.Model, tea.Cmd) {
	m.filterOpen = true
	m.status = ""

	return m, nil
}

// handleFilterKey edits the query while the prompt is open. Every printable key
// belongs to the query, so the keymap cannot fire from inside the prompt.
func (m Model) handleFilterKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyCtrlC:
		return m, tea.Quit

	case tea.KeyEsc:
		m.filterOpen = false
		m.filter = ""
		m.status = ""

		return m.applyFilterAndFollow()

	case tea.KeyEnter:
		m.filterOpen = false
		// A query of nothing but spaces filters nothing, so committing it would
		// leave "filter:   " in the footer and no narrowing on screen.
		m.filter = strings.TrimSpace(m.filter)

		return m.applyFilterAndFollow()

	case tea.KeyBackspace:
		if runes := []rune(m.filter); len(runes) > 0 {
			m.filter = string(runes[:len(runes)-1])

			return m.applyFilterAndFollow()
		}

	case tea.KeyRunes:
		m.filter += string(msg.Runes)

		return m.applyFilterAndFollow()

	case tea.KeySpace:
		m.filter += " "

		return m.applyFilterAndFollow()
	}

	return m, nil
}

// applyFilterAndFollow narrows the rows and, when the reading pane has focus, opens
// the message the cursor landed on: a filtered list must never leave the pane
// showing a body that belongs to a hidden message.
func (m Model) applyFilterAndFollow() (tea.Model, tea.Cmd) {
	m.applyFilter()

	next, cmd := m.followRows()

	return next, cmd
}

// renderFilterModal centres the filter prompt in the content area, the same shape
// as the delete confirmation: a title, the thing being edited, and one line of
// hints. The count is what makes the popup worth having without dismissing it — it
// answers "is this narrowing anything" on every keystroke.
func (m Model) renderFilterModal(width, height int) string {
	count := fmt.Sprintf("%d of %s", len(m.rows), countLabel(len(m.messages), "message"))
	if len(m.rows) == 0 && strings.TrimSpace(m.filter) != "" {
		count = "no matches"
	}

	// The dialog's own chrome: one border cell and two padding cells per side. A
	// typed query has no natural bound, and the frame is only clipped by the
	// terminal, so the two lines that grow are truncated rather than allowed to
	// widen the whole view.
	inner := max(1, width-6)

	lines := []string{
		TitleStyle.Render("filter"),
		"",
		NormalStyle.Render(TruncateDisplay("/"+m.filter+"_", inner)),
		MutedStyle.Render(TruncateDisplay(count, inner)),
		"",
		MutedStyle.Render("enter:keep  esc:clear"),
	}

	modal := ModalStyle.Render(strings.Join(lines, "\n"))

	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, modal)
}
