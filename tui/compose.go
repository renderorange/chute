package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/renderorange/chute/mail"
)

// composerAction is what the composer wants the model to do next. The composer decides
// intent; the model owns the side effects, because only it can report a partial send.
type composerAction int

const (
	composerNothing composerAction = iota
	composerClose
	composerConfirmClose
	composerSave
	composerSend
)

// Composer focus order.
const (
	composerTo = iota
	composerCc
	composerSubject
	composerBody
	composerFields = 4
)

// composer is the in-pane writing surface: three single-line header fields and a body.
type composer struct {
	to      *editor
	cc      *editor
	subject *editor
	body    *editor

	focus  int
	dirty  bool
	scroll int

	// id is the Message-ID for this message, fixed when the composer opens so the
	// draft and the sent copy carry the same one.
	id string
}

// newComposer builds a composer from a pre-filled message. bottom opens the body at its
// end, which is where a bottom-posted reply belongs.
func newComposer(initial mail.Compose, bottom bool) *composer {
	c := &composer{
		to:      newEditor(true),
		cc:      newEditor(true),
		subject: newEditor(true),
		body:    newEditor(false),
	}

	c.to.setText(strings.Join(initial.To, ", "))
	c.cc.setText(strings.Join(initial.Cc, ", "))
	c.subject.setText(initial.Subject)
	c.body.setText(initial.Text)
	c.id = initial.MessageID

	if bottom {
		c.body.cursorEnd()
	} else {
		c.body.cursorStart()
	}

	return c
}

// field returns the editor that has focus.
func (c *composer) field() *editor {
	switch c.focus {
	case composerTo:
		return c.to
	case composerCc:
		return c.cc
	case composerSubject:
		return c.subject
	}

	return c.body
}

// compose returns the message as it stands. The Message-ID is generated once, by the
// model, and carried through every save and send of this message.
func (c *composer) compose() mail.Compose {
	return mail.Compose{
		To:        splitField(c.to.text()),
		Cc:        splitField(c.cc.text()),
		Subject:   c.subject.text(),
		Text:      c.body.text(),
		MessageID: c.id,
	}
}

// update applies one keypress. It never consults the global keymap, so q types q and
// nothing here can quit the program.
func (c *composer) update(msg tea.KeyMsg) composerAction {
	switch msg.String() {
	case "ctrl+s":
		return composerSave
	case "ctrl+d":
		return composerSend
	case "esc":
		if c.dirty {
			return composerConfirmClose
		}

		return composerClose
	case "tab":
		c.focus = (c.focus + 1) % composerFields

		return composerNothing
	case "shift+tab":
		c.focus = (c.focus + composerFields - 1) % composerFields

		return composerNothing
	case "enter":
		if c.focus != composerBody {
			c.focus++
			return composerNothing
		}
		c.body.insert("\n")
		c.dirty = true

		return composerNothing
	case "backspace":
		c.field().backspace()
		c.dirty = true

		return composerNothing
	case "left":
		c.field().moveLeft()

		return composerNothing
	case "right":
		c.field().moveRight()

		return composerNothing
	case "up":
		c.field().moveUp()

		return composerNothing
	case "down":
		c.field().moveDown()

		return composerNothing
	case "home":
		c.field().cursorHome()

		return composerNothing
	case "end":
		c.field().cursorLineEnd()

		return composerNothing
	}

	if msg.Type == tea.KeyRunes {
		c.field().insert(string(msg.Runes))
		c.dirty = true
	}

	return composerNothing
}

// view renders the composer body for a panel of the given inner size. The cursor is
// drawn as a reversed cell because the terminal cursor is not used anywhere in the app.
func (c *composer) view(width, height int) []string {
	rows := []string{
		c.fieldLine("To:      ", c.to, composerTo, width),
		c.fieldLine("Cc:      ", c.cc, composerCc, width),
		c.fieldLine("Subject: ", c.subject, composerSubject, width),
		"",
	}

	bodyRows, cursorRow, cursorCol := c.body.display(width)

	available := height - len(rows)
	if available < 1 {
		available = 1
	}

	if cursorRow >= c.scroll+available {
		c.scroll = cursorRow - available + 1
	}
	if cursorRow < c.scroll {
		c.scroll = cursorRow
	}
	if c.scroll > len(bodyRows)-1 {
		c.scroll = max(0, len(bodyRows)-1)
	}

	for i := c.scroll; i < len(bodyRows) && len(rows) < height; i++ {
		line := bodyRows[i]
		if c.focus == composerBody && i == cursorRow {
			line = withCursor(line, cursorCol)
		}
		rows = append(rows, line)
	}

	return rows
}

// fieldLine renders one header row, with a cursor when that field has focus.
func (c *composer) fieldLine(label string, field *editor, which, width int) string {
	value := field.text()
	if c.focus == which {
		value = withCursor(value, field.col)
	}

	line := label + value

	return TruncateDisplay(line, width)
}

// withCursor draws a reversed cell at col, padding with a space past the end of the
// text so the cursor is visible in an empty field.
func withCursor(text string, col int) string {
	runes := []rune(text)
	if col > len(runes) {
		col = len(runes)
	}

	before := string(runes[:col])

	at := " "
	after := ""
	if col < len(runes) {
		at = string(runes[col])
		after = string(runes[col+1:])
	}

	return before + lipgloss.NewStyle().Reverse(true).Render(at) + after
}

// splitField turns a header field into individual addresses, dropping blanks.
func splitField(value string) []string {
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))

	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			out = append(out, trimmed)
		}
	}

	return out
}
