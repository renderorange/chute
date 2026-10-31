package tui

import (
	"fmt"
	"strings"

	"github.com/renderorange/chute/mail"
)

// readerRows is how many reader lines fit below the panel title right now.
func (m Model) readerRows() int {
	if m.width == 0 || m.height == 0 {
		return 1
	}

	rows := ComputeLayout(m.width, m.height).Reader.Height - 1
	if rows < 1 {
		return 1
	}

	return rows
}

// readerInnerWidth is the reader's usable text width in the current layout.
func (m Model) readerInnerWidth() int {
	if m.width == 0 || m.height == 0 {
		return 1
	}

	width := ComputeLayout(m.width, m.height).Reader.Width - (2 * PanelPaddingHorizontal)
	if width < 1 {
		return 1
	}

	return width
}

// readerLines renders the conversation the cursor is in: every message, oldest
// first, with the body expanded under the message the cursor is on. Bodies are
// cached per message, so walking a thread costs one fetch per reply.
func (m Model) readerLines(width int) []string {
	msg, ok := m.selectedMessage()
	if !ok {
		return []string{MutedStyle.Render("no message selected")}
	}

	thread := m.currentThread()
	key := m.selectedBodyKey()

	lines := make([]string, 0, 8)
	for i, row := range m.rows {
		if row.Thread != thread {
			continue
		}
		if i != m.messageCursor {
			lines = append(lines, threadLine(row.Message, width))
			continue
		}

		lines = append(lines,
			NormalStyle.Render(Truncate(row.Message.Subject(), width)),
			MutedStyle.Render(Truncate("from "+row.Message.From(), width)),
			MutedStyle.Render(row.Message.Date().Local().Format("Mon 02 Jan 2006 15:04")),
		)
		lines = append(lines, m.bodyLines(key, width)...)
	}

	// selectedMessage succeeded, so the loop always emitted the expanded message;
	// the fallback keeps the reader honest if that ever stops being true.
	if len(lines) == 0 {
		return []string{NormalStyle.Render(Truncate(msg.Subject(), width))}
	}

	return lines
}

// bodyLines renders the expanded message's body and attachments.
func (m Model) bodyLines(key bodyKey, width int) []string {
	if m.loadingBody == key {
		return []string{Blank(1), MutedStyle.Render("loading the body...")}
	}

	body, ok := m.bodyFor(key)
	if !ok {
		return []string{Blank(1), MutedStyle.Render("enter to load the body")}
	}

	text := body.Text
	if strings.TrimSpace(text) == "" {
		text = "(no text body)"
	}

	lines := []string{Blank(1)}
	for _, line := range wrapText(text, width) {
		lines = append(lines, NormalStyle.Render(line))
	}

	if len(body.Attachments) > 0 {
		lines = append(lines, Blank(1), TitleStyle.Render("attachments"))
		for i, attachment := range body.Attachments {
			label := fmt.Sprintf("%d  %s  (%s)", i+1, attachment.Filename, formatSize(len(attachment.Data)))
			lines = append(lines, MutedStyle.Render(Truncate(label, width)))
		}
		lines = append(lines, MutedStyle.Render("s:save attachment"))
	}

	return lines
}

// currentThread is the conversation the cursor sits in, or -1 when there is no
// message list.
func (m Model) currentThread() int {
	if m.messageCursor < 0 || m.messageCursor >= len(m.rows) {
		return -1
	}

	return m.rows[m.messageCursor].Thread
}

// threadLine is the one-line header shown for the other messages of a thread.
func threadLine(msg mail.Message, width int) string {
	date := msg.Date().Local().Format("Jan 02")
	label := fmt.Sprintf("%s  %s  %s", date, msg.From(), msg.Subject())

	return MutedStyle.Render(Truncate(label, width))
}

// readerWindow slices the reader content to the scroll offset and panel height.
func (m Model) readerWindow(box Box, lines []string) []string {
	rows := box.Height - 1
	if rows < 1 {
		return nil
	}

	start := clamp(m.readerScroll, 0, max(0, len(lines)-rows))
	end := min(start+rows, len(lines))

	return lines[start:end]
}

// moveReaderScroll scrolls the reader by delta lines.
func (m *Model) moveReaderScroll(delta int) {
	total := len(m.readerLines(m.readerInnerWidth()))
	rows := m.readerRows()

	m.readerScroll = clamp(m.readerScroll+delta, 0, max(0, total-rows))
}

// clampReaderScroll keeps the stored offset valid after a cursor move, a body
// load or a resize.
func (m *Model) clampReaderScroll() {
	total := len(m.readerLines(m.readerInnerWidth()))
	rows := m.readerRows()

	m.readerScroll = clamp(m.readerScroll, 0, max(0, total-rows))
}

// selectedBodyKey identifies the body for the message under the cursor.
func (m Model) selectedBodyKey() bodyKey {
	msg, ok := m.selectedMessage()
	if !ok {
		return bodyKey{}
	}

	return bodyKey{account: m.accountName(), folder: m.openFolder, uid: msg.UID}
}

// bodyFor returns the cached body for a key.
func (m Model) bodyFor(key bodyKey) (*mail.Body, bool) {
	body, ok := m.bodies[key]

	return body, ok
}

// formatSize renders a byte count for the attachment list.
func formatSize(n int) string {
	switch {
	case n < 1024:
		return fmt.Sprintf("%d B", n)
	case n < 1024*1024:
		return fmt.Sprintf("%.1f KB", float64(n)/1024)
	default:
		return fmt.Sprintf("%.1f MB", float64(n)/(1024*1024))
	}
}

// wrapText word-wraps plain text to width, preserving explicit line breaks.
func wrapText(text string, width int) []string {
	if width < 1 {
		return nil
	}

	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")

	var lines []string
	for _, paragraph := range strings.Split(text, "\n") {
		lines = append(lines, wrapLine(paragraph, width)...)
	}

	for len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}

	return lines
}

// wrapLine wraps one logical line, hard-breaking a word longer than width.
func wrapLine(line string, width int) []string {
	line = strings.ReplaceAll(line, "\t", "    ")
	if line == "" {
		return []string{""}
	}

	var lines []string
	remaining := []rune(line)
	for len(remaining) > width {
		cut := width
		for i := width; i > 0; i-- {
			if remaining[i] == ' ' {
				cut = i
				break
			}
		}
		if cut <= 0 {
			cut = width
		}

		lines = append(lines, strings.TrimRight(string(remaining[:cut]), " "))
		remaining = remaining[cut:]
		if len(remaining) > 0 && remaining[0] == ' ' {
			remaining = remaining[1:]
		}
	}

	return append(lines, string(remaining))
}
