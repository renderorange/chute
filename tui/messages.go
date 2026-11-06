package tui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/emersion/go-imap/v2"
	"github.com/renderorange/chute/config"
	"github.com/renderorange/chute/mail"
)

// sortNewestFirst orders messages by date descending, so the newest is on top, and
// falls back to UID descending for messages with the same date. UID alone is
// arrival order, not chronology: a message moved into the folder gets a high UID
// and would sort as the newest despite being old.
func sortNewestFirst(messages []mail.Message) []mail.Message {
	sort.Slice(messages, func(i, j int) bool {
		left, right := messages[i], messages[j]

		if !left.Date().Equal(right.Date()) {
			return left.Date().After(right.Date())
		}

		return left.UID > right.UID
	})

	return messages
}

// messageRows is how many message lines fit in the middle panel right now.
func (m Model) messageRows() int {
	if m.width == 0 || m.height == 0 {
		return 1
	}

	layout := m.layout()
	box := layout.Messages
	if box.Empty() {
		box = layout.Reader
	}

	rows := box.Height - 1
	if rows < 1 {
		return 1
	}

	return rows
}

// moveMessageCursor moves the selection and keeps it inside the scroll window.
func (m *Model) moveMessageCursor(delta int) {
	if len(m.rows) == 0 {
		return
	}

	m.messageCursor = clamp(m.messageCursor+delta, 0, len(m.rows)-1)

	rows := m.messageRows()
	if m.messageCursor < m.messageScroll {
		m.messageScroll = m.messageCursor
	}
	if m.messageCursor >= m.messageScroll+rows {
		m.messageScroll = m.messageCursor - rows + 1
	}

	m.readerScroll = 0
}

// clampMessageScroll keeps the stored offset valid after a reload or resize.
func (m *Model) clampMessageScroll() {
	rows := m.messageRows()
	maxScroll := max(0, len(m.rows)-rows)

	m.messageScroll = clamp(m.messageScroll, 0, maxScroll)
	m.messageCursor = clamp(m.messageCursor, 0, max(0, len(m.rows)-1))

	if m.messageCursor < m.messageScroll {
		m.messageScroll = m.messageCursor
	}
	if m.messageCursor >= m.messageScroll+rows {
		m.messageScroll = m.messageCursor - rows + 1
	}
}

// messageLines renders the visible slice of the message list.
func (m Model) messageLines(rows, width int) []string {
	if rows < 1 {
		return nil
	}

	switch {
	case m.msgLoading:
		return []string{MutedStyle.Render("loading...")}
	case m.openFolder == "":
		return []string{MutedStyle.Render("open a folder with enter")}
	case len(m.rows) == 0:
		if query := strings.TrimSpace(m.filter); query != "" {
			return []string{MutedStyle.Render(Truncate(fmt.Sprintf("no messages match %q", query), width))}
		}

		return []string{MutedStyle.Render("no messages")}
	}

	start := clamp(m.messageScroll, 0, max(0, len(m.rows)-rows))
	end := min(start+rows, len(m.rows))

	lines := make([]string, 0, end-start)
	for i := start; i < end; i++ {
		lines = append(lines, m.messageLine(i, width))
	}

	return lines
}

// Gaps and fixed widths of a message row. The date is the only column with a wider
// gap before it, which is what makes it read as a separate column rather than a third
// field; every other gap is one space.
const (
	messageGap = 3

	columnGap = 1

	// rowPrefixCells is the selection marker, the unread and flagged markers, and the
	// space after them.
	rowPrefixCells = 3

	// minSubjectWidth is the narrowest the subject may become before the indent is
	// given up instead.
	minSubjectWidth = 4
)

// gapBefore is the gap between a column and the one before it.
func gapBefore(col string) int {
	if col == config.ListColumnDate {
		return messageGap
	}

	return columnGap
}

// listColumns is the configured column order, or the shipped one when [list] is absent.
func (m Model) listColumns() []string {
	return m.cfg.List.ColumnOrder()
}

// formatListSize renders a byte count for the size column: binary units, no space, and
// one decimal below 100 units. The units match formatSize's, so one message reads the
// same in the list and in the attachment list (#63).
func formatListSize(n int64) string {
	if n < 0 {
		n = 0
	}
	if n < 1024 {
		return fmt.Sprintf("%dB", n)
	}

	value := float64(n)
	unit := "K"

	for _, candidate := range []string{"K", "M", "G"} {
		unit = candidate
		value /= 1024
		if value < 1024 {
			break
		}
	}

	// At 100 units and above the decimal only buys a rounding artefact ("1024.0K"); below
	// it, "12.3K" and "6.8M" are worth the cell.
	if value < 100 {
		return fmt.Sprintf("%.1f%s", value, unit)
	}

	return fmt.Sprintf("%.0f%s", value, unit)
}

// sizeCell right-aligns the size in its fixed column. A value past the cap loses leading
// digits rather than widening the row, which would wrap the list.
func sizeCell(n int64) string {
	value := formatListSize(n)
	if width := lipgloss.Width(value); width > config.ListMaxSizeCells {
		runes := []rune(value)
		value = string(runes[len(runes)-config.ListMaxSizeCells:])
	}

	return strings.Repeat(" ", config.ListMaxSizeCells-lipgloss.Width(value)) + value
}

// messageLine renders one message row: markers, then the configured columns in order,
// with the subject absorbing whatever width is left.
func (m Model) messageLine(index, width int) string {
	msg := m.rows[index].Message

	// ASCII markers keep every row one cell wide, so the date never wraps. A collapsed
	// thread's root borrows the unread marker when any reply it hides is unread, so a
	// fold cannot make mail look read (#59).
	unread := " "
	if !msg.Seen() || m.rows[index].HiddenUnread {
		unread = "*"
	}
	flagged := " "
	if msg.Flagged() {
		flagged = "!"
	}
	if index == m.messageCursor {
		unread = ">"
	}

	columns := m.listColumns()

	// The date is truncated and measured in display cells, not bytes: the layout comes
	// from config, so a wide rune is user input and could otherwise widen the row.
	date := TruncateDisplay(m.cfg.List.FormatDate(msg.Date().Local()), config.ListMaxDateCells)

	// A collapsed root says how many replies it hides; the subject absorbs the cost.
	subject := singleLine(msg.Subject())
	if m.rows[index].Hidden > 0 {
		subject = fmt.Sprintf("%s (%d)", subject, m.rows[index].Hidden)
	}

	// Widths of the columns that do not absorb space.
	fixed := make(map[string]int, len(columns))
	for _, col := range columns {
		switch col {
		case config.ListColumnFrom:
			fixed[col] = clamp(width/4, 10, 20)
		case config.ListColumnSize:
			fixed[col] = config.ListMaxSizeCells
		case config.ListColumnDate:
			fixed[col] = lipgloss.Width(date)
		}
	}

	// Indentation comes out of the subject budget, never out of the row: two cells
	// per depth, capped, and given up entirely before the subject is squeezed.
	indent := 2 * min(m.rows[index].Depth, MaxThreadDepth)

	base := rowPrefixCells
	for i, col := range columns {
		if i > 0 {
			base += gapBefore(col)
		}
		if col != config.ListColumnSubject {
			base += fixed[col]
		}
	}

	indent = min(indent, max(0, width-base-minSubjectWidth))
	subjectWidth := max(minSubjectWidth, width-base-indent)

	var row strings.Builder

	row.WriteString(unread)
	row.WriteString(flagged)
	row.WriteString(" ")
	row.WriteString(strings.Repeat(" ", indent))

	for i, col := range columns {
		if i > 0 {
			row.WriteString(strings.Repeat(" ", gapBefore(col)))
		}

		switch col {
		case config.ListColumnFrom:
			row.WriteString(padColumn(singleLine(msg.From()), fixed[col]))
		case config.ListColumnSubject:
			row.WriteString(padColumn(subject, subjectWidth))
		case config.ListColumnSize:
			row.WriteString(sizeCell(msg.Size))
		case config.ListColumnDate:
			row.WriteString(date)
		}
	}

	line := row.String()

	// Bold is the selection indicator and nothing else (#97). Unread mail keeps
	// the marker column's "*", which survives in monochrome and does not compete
	// with the cursor or a range for the one bold attribute.
	if index == m.messageCursor {
		return SelectedStyle.Render(line)
	}
	if m.inVisual(index) {
		return SelectedStyle.Render(line)
	}

	return NormalStyle.Render(line)
}

// selectedMessage is the message under the cursor, if there is one.
func (m Model) selectedMessage() (mail.Message, bool) {
	if m.messageCursor < 0 || m.messageCursor >= len(m.rows) {
		return mail.Message{}, false
	}

	return m.rows[m.messageCursor].Message, true
}

// rebuildRows re-derives the list after messages changed and keeps the cursor on
// the same message, identified by UID. Re-parenting and reordering must not
// silently move the selection to a different conversation. The active filter is
// applied here too, so a flag change or a delete cannot drop it.
func (m *Model) rebuildRows() {
	keep, hasCursor := m.selectedMessage()
	rows, hiddenBy := collapseRows(m.visibleRows(), m.collapsed, m.filter == "")
	m.rows = rows

	if hasCursor {
		index := rowIndex(rows, keep.UID)
		if index < 0 {
			if root, ok := hiddenBy[keep.UID]; ok {
				index = rowIndex(rows, root)
			}
		}
		if index >= 0 {
			m.messageCursor = index
		}
	}

	m.clampMessageScroll()
}

// rowIndex finds the row holding uid, or -1.
func rowIndex(rows []Row, uid imap.UID) int {
	for i, row := range rows {
		if row.Message.UID == uid {
			return i
		}
	}

	return -1
}
