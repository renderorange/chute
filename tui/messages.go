package tui

import (
	"fmt"
	"sort"

	"github.com/renderorange/chute/mail"
)

// sortNewestFirst orders messages by UID descending, so the newest is on top.
func sortNewestFirst(messages []mail.Message) []mail.Message {
	sort.Slice(messages, func(i, j int) bool {
		return messages[i].UID > messages[j].UID
	})

	return messages
}

// messageRows is how many message lines fit in the middle panel right now.
func (m Model) messageRows() int {
	if m.width == 0 || m.height == 0 {
		return 1
	}

	layout := ComputeLayout(m.width, m.height)
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
	if len(m.messages) == 0 {
		return
	}

	m.messageCursor = clamp(m.messageCursor+delta, 0, len(m.messages)-1)

	rows := m.messageRows()
	if m.messageCursor < m.messageScroll {
		m.messageScroll = m.messageCursor
	}
	if m.messageCursor >= m.messageScroll+rows {
		m.messageScroll = m.messageCursor - rows + 1
	}
}

// clampMessageScroll keeps the stored offset valid after a reload or resize.
func (m *Model) clampMessageScroll() {
	rows := m.messageRows()
	maxScroll := max(0, len(m.messages)-rows)

	m.messageScroll = clamp(m.messageScroll, 0, maxScroll)
	m.messageCursor = clamp(m.messageCursor, 0, max(0, len(m.messages)-1))

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
		return []string{MutedStyle.Render("loading…")}
	case m.openFolder == "":
		return []string{MutedStyle.Render("open a folder with enter")}
	case len(m.messages) == 0:
		return []string{MutedStyle.Render("no messages")}
	}

	start := clamp(m.messageScroll, 0, max(0, len(m.messages)-rows))
	end := min(start+rows, len(m.messages))

	lines := make([]string, 0, end-start)
	for i := start; i < end; i++ {
		lines = append(lines, m.messageLine(i, width))
	}

	return lines
}

// messageLine renders one message row: markers, sender, subject, date.
func (m Model) messageLine(index, width int) string {
	msg := m.messages[index]

	// Two marker columns: unread dot, then flag.
	unread := " "
	if !msg.Seen() {
		unread = "●"
	}
	flagged := " "
	if msg.Flagged() {
		flagged = "!"
	}
	if index == m.messageCursor {
		unread = "▸"
	}

	date := msg.Date().Local().Format("Jan 02")
	fromWidth := clamp(width/4, 10, 20)
	subjectWidth := max(4, width-fromWidth-len(date)-5)

	line := fmt.Sprintf("%s%s %-*s %-*s %s",
		unread, flagged,
		fromWidth, Truncate(msg.From(), fromWidth),
		subjectWidth, Truncate(msg.Subject(), subjectWidth),
		date,
	)

	if index == m.messageCursor {
		return SelectedStyle.Render(line)
	}
	if !msg.Seen() {
		return UnreadStyle.Render(line)
	}

	return NormalStyle.Render(line)
}

// selectedMessage is the message under the cursor, if there is one.
func (m Model) selectedMessage() (mail.Message, bool) {
	if m.messageCursor < 0 || m.messageCursor >= len(m.messages) {
		return mail.Message{}, false
	}

	return m.messages[m.messageCursor], true
}
