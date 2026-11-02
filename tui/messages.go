package tui

import (
	"fmt"
	"sort"
	"strings"

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

// messageGap is the spaces between the subject field and the date.
const messageGap = 3

// messageLine renders one message row: markers, sender, subject, gap, date.
func (m Model) messageLine(index, width int) string {
	msg := m.rows[index].Message

	// ASCII markers keep every row one cell wide, so the date never wraps.
	unread := " "
	if !msg.Seen() {
		unread = "*"
	}
	flagged := " "
	if msg.Flagged() {
		flagged = "!"
	}
	if index == m.messageCursor {
		unread = ">"
	}

	date := msg.Date().Local().Format("Jan 02")
	fromWidth := clamp(width/4, 10, 20)

	// Indentation comes out of the subject budget, never out of the row: two cells
	// per depth, capped, and given up entirely if the row is too narrow to hold a
	// subject afterwards.
	indent := 2 * min(m.rows[index].Depth, MaxThreadDepth)
	if limit := width - fromWidth - len(date) - messageGap - 4 - 4; indent > limit {
		indent = max(0, limit)
	}

	subjectWidth := width - fromWidth - len(date) - messageGap - 4 - indent
	if subjectWidth < 4 {
		subjectWidth = 4
	}

	line := fmt.Sprintf("%s%s %s%s %s%s%s",
		unread, flagged,
		strings.Repeat(" ", indent),
		padColumn(singleLine(msg.From()), fromWidth),
		padColumn(singleLine(msg.Subject()), subjectWidth),
		strings.Repeat(" ", messageGap),
		date,
	)

	if index == m.messageCursor {
		return SelectedStyle.Render(line)
	}
	if m.inVisual(index) {
		return SelectedStyle.Render(line)
	}
	if !msg.Seen() {
		return UnreadStyle.Render(line)
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
// silently move the selection to a different conversation.
func (m *Model) rebuildRows() {
	keep, hasCursor := m.selectedMessage()

	m.rows = BuildRows(m.messages)

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
