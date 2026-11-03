package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"

	"github.com/renderorange/chute/mail"
)

// key builds a rune keypress, the way bubbletea delivers typing.
func key(r rune) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}}
}

// namedKey builds a key with a name, such as "tab" or "ctrl+s".
func namedKey(name string) tea.KeyMsg {
	switch name {
	case "tab":
		return tea.KeyMsg{Type: tea.KeyTab}
	case "shift+tab":
		return tea.KeyMsg{Type: tea.KeyShiftTab}
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "backspace":
		return tea.KeyMsg{Type: tea.KeyBackspace}
	case "left":
		return tea.KeyMsg{Type: tea.KeyLeft}
	case "right":
		return tea.KeyMsg{Type: tea.KeyRight}
	case "up":
		return tea.KeyMsg{Type: tea.KeyUp}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	case "home":
		return tea.KeyMsg{Type: tea.KeyHome}
	case "end":
		return tea.KeyMsg{Type: tea.KeyEnd}
	case "ctrl+s":
		return tea.KeyMsg{Type: tea.KeyCtrlS}
	case "ctrl+d":
		return tea.KeyMsg{Type: tea.KeyCtrlD}
	}

	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(name)}
}

// TestComposerTypesIntoTheFocusedField proves a plain letter is text, not a binding.
// This is the M3 lesson applied to a third key mode.
func TestComposerTypesIntoTheFocusedField(t *testing.T) {
	c := newComposer(mail.Compose{}, false)

	c.update(key('q'))
	c.update(key('m'))

	if got := c.to.text(); got != "qm" {
		t.Errorf("To field = %q, want %q: the composer read a global binding", got, "qm")
	}

	c.update(namedKey("tab"))
	c.update(namedKey("tab"))
	c.update(namedKey("tab"))
	c.update(namedKey("tab"))
	c.update(key('q'))

	if got := c.compose().Text; got != "q" {
		t.Errorf("body = %q, want %q", got, "q")
	}
}

// TestComposerTabCyclesFields covers the focus order and the wrap.
func TestComposerTabCyclesFields(t *testing.T) {
	c := newComposer(mail.Compose{}, false)

	want := []int{composerCc, composerBcc, composerSubject, composerBody, composerTo}
	for _, expected := range want {
		c.update(namedKey("tab"))
		if c.focus != expected {
			t.Fatalf("focus = %d, want %d", c.focus, expected)
		}
	}

	c.update(namedKey("shift+tab"))
	if c.focus != composerBody {
		t.Errorf("shift+tab from To landed on %d, want body", c.focus)
	}
}

// TestComposerEnterMovesBetweenHeaderFields proves Enter is a field advance until the
// body, where it inserts a newline.
func TestComposerEnterMovesBetweenHeaderFields(t *testing.T) {
	c := newComposer(mail.Compose{}, false)

	c.update(namedKey("enter"))
	if c.focus != composerCc {
		t.Errorf("enter from To landed on %d, want Cc", c.focus)
	}

	c.update(namedKey("enter"))
	c.update(namedKey("enter"))
	c.update(namedKey("enter"))
	if c.focus != composerBody {
		t.Fatalf("focus = %d, want the body", c.focus)
	}

	c.update(key('a'))
	c.update(namedKey("enter"))
	c.update(key('b'))

	if got := c.compose().Text; got != "a\nb" {
		t.Errorf("body = %q, want %q", got, "a\nb")
	}
}

// TestComposerEscapeDependsOnDirt proves a clean composer closes silently and a dirty
// one asks first, which is what stops a paragraph disappearing.
func TestComposerEscapeDependsOnDirt(t *testing.T) {
	clean := newComposer(mail.Compose{}, false)
	if got := clean.update(namedKey("esc")); got != composerClose {
		t.Errorf("esc on a clean composer returned %v, want composerClose", got)
	}

	dirty := newComposer(mail.Compose{}, false)
	dirty.update(key('x'))
	if got := dirty.update(namedKey("esc")); got != composerConfirmClose {
		t.Errorf("esc on a dirty composer returned %v, want composerConfirmClose", got)
	}
}

// TestComposerActions covers the two explicit commands.
func TestComposerActions(t *testing.T) {
	c := newComposer(mail.Compose{}, false)

	if got := c.update(namedKey("ctrl+s")); got != composerSave {
		t.Errorf("ctrl+s returned %v, want composerSave", got)
	}
	if got := c.update(namedKey("ctrl+d")); got != composerSend {
		t.Errorf("ctrl+d returned %v, want composerSend", got)
	}
}

// TestComposerComposeReflectsEdits proves the fields reach the outgoing message and that
// a comma-separated field becomes separate recipients.
func TestComposerComposeReflectsEdits(t *testing.T) {
	c := newComposer(mail.Compose{MessageID: "<kept@example.org>"}, false)

	for _, r := range "one@example.com, Two <two@example.com>" {
		c.update(key(r))
	}

	c.update(namedKey("tab"))
	for _, r := range "cc@example.com" {
		c.update(key(r))
	}

	c.update(namedKey("tab"))
	for _, r := range "blind@example.com" {
		c.update(key(r))
	}

	c.update(namedKey("tab"))
	for _, r := range "the subject" {
		c.update(key(r))
	}

	got := c.compose()
	if len(got.To) != 2 || got.To[0] != "one@example.com" || got.To[1] != "Two <two@example.com>" {
		t.Errorf("To = %v", got.To)
	}
	if len(got.Cc) != 1 || got.Cc[0] != "cc@example.com" {
		t.Errorf("Cc = %v", got.Cc)
	}
	if len(got.Bcc) != 1 || got.Bcc[0] != "blind@example.com" {
		t.Errorf("Bcc = %v", got.Bcc)
	}
	if got.Subject != "the subject" {
		t.Errorf("Subject = %q", got.Subject)
	}
}

// TestComposerOpensAtTheConfiguredEnd proves reply_position decides where the cursor
// lands in the body.
func TestComposerOpensAtTheConfiguredEnd(t *testing.T) {
	initial := mail.Compose{Text: "reply area\n\n> quoted"}

	top := newComposer(initial, false)
	if top.body.row != 0 || top.body.col != 0 {
		t.Errorf("top-positioned cursor = (%d,%d), want (0,0)", top.body.row, top.body.col)
	}

	bottom := newComposer(initial, true)
	if bottom.body.row != 2 {
		t.Errorf("bottom-positioned cursor row = %d, want the last line", bottom.body.row)
	}
}

// TestComposerViewShowsFieldsAndFits proves the labels are drawn and no row exceeds the
// width, at both the tablet size and the M6 floor.
func TestComposerViewShowsFieldsAndFits(t *testing.T) {
	c := newComposer(mail.Compose{
		To:      []string{"one@example.com"},
		Cc:      []string{"two@example.com"},
		Bcc:     []string{"three@example.com"},
		Subject: "a subject",
		Text:    "a body line",
	}, false)

	for _, size := range []struct{ width, height int }{{99, 20}, {54, 12}} {
		rows := c.view(size.width, size.height)

		if len(rows) > size.height {
			t.Errorf("%dx%d: %d rows, want at most %d", size.width, size.height, len(rows), size.height)
		}

		joined := strings.Join(rows, "\n")
		for _, want := range []string{"To:", "Cc:", "Bcc:", "Subject:", "a body line"} {
			if !strings.Contains(joined, want) {
				t.Errorf("%dx%d: the view is missing %q:\n%s", size.width, size.height, want, joined)
			}
		}

		for i, row := range rows {
			if got := runewidth.StringWidth(row); got > size.width {
				t.Errorf("%dx%d: row %d is %d cells wide: %q", size.width, size.height, i, got, row)
			}
		}
	}
}

// TestComposerEditingKeys proves the composer routes arrows, home, end and backspace to
// the focused field rather than to the model.
func TestComposerEditingKeys(t *testing.T) {
	c := newComposer(mail.Compose{}, false)
	c.to.setText("abc")
	c.to.cursorLineEnd()

	c.update(namedKey("left"))
	c.update(namedKey("backspace"))
	if got := c.to.text(); got != "ac" {
		t.Fatalf("To = %q, want %q after left then backspace", got, "ac")
	}

	c.update(namedKey("home"))
	c.update(key('z'))
	if got := c.to.text(); got != "zac" {
		t.Errorf("To = %q, want %q after home then z", got, "zac")
	}

	c.update(namedKey("end"))
	c.update(key('!'))
	if got := c.to.text(); got != "zac!" {
		t.Errorf("To = %q, want %q after end then !", got, "zac!")
	}

	if !c.dirty {
		t.Error("editing did not mark the composer dirty")
	}
}

// TestComposerScrollsToKeepTheCursorVisible proves a body longer than the panel keeps
// the cursor on screen instead of drawing the top and leaving it invisible.
func TestComposerScrollsToKeepTheCursorVisible(t *testing.T) {
	lines := make([]string, 40)
	for i := range lines {
		lines[i] = "line"
	}

	c := newComposer(mail.Compose{Text: strings.Join(lines, "\n")}, true)

	rows := c.view(40, 10)
	if len(rows) > 10 {
		t.Errorf("%d rows, want at most 10", len(rows))
	}
	if c.scroll == 0 {
		t.Error("the composer did not scroll to the cursor at the end of a long body")
	}
}

// TestComposerRoutesEveryArrowToTheFocusedField proves right, up and down move the cursor
// in the focused editor. Left is covered above; these three are the rest of the pair, and
// a global binding reaching them would move the list instead of the text.
func TestComposerRoutesEveryArrowToTheFocusedField(t *testing.T) {
	c := newComposer(mail.Compose{Text: "ab\ncd"}, false)
	c.focus = composerBody

	c.update(namedKey("right"))
	if c.body.col != 1 {
		t.Errorf("right left the cursor at column %d, want 1", c.body.col)
	}

	c.update(namedKey("down"))
	if c.body.row != 1 {
		t.Errorf("down left the cursor on row %d, want 1", c.body.row)
	}

	c.update(namedKey("up"))
	if c.body.row != 0 {
		t.Errorf("up left the cursor on row %d, want 0", c.body.row)
	}

	if got := c.body.text(); got != "ab\ncd" {
		t.Errorf("body = %q, want the arrows to move the cursor and not edit", got)
	}
	if c.dirty {
		t.Error("moving the cursor marked the composer dirty")
	}
}

// TestComposerCursorMarksTheCharacterUnderIt proves the reversed cell sits on the
// character at the cursor, and becomes a trailing space when the cursor is past the end,
// which is what makes an empty field look focused.
func TestComposerCursorMarksTheCharacterUnderIt(t *testing.T) {
	reversed := func(s string) string { return lipgloss.NewStyle().Reverse(true).Render(s) }

	under := withCursor("ab", 0)
	if want := reversed("a") + "b"; under != want {
		t.Errorf("cursor on the first character = %q, want %q", under, want)
	}

	past := withCursor("ab", 9)
	if want := "ab" + reversed(" "); past != want {
		t.Errorf("cursor past the end = %q, want %q", past, want)
	}
	if got := lipgloss.Width(past); got != 3 {
		t.Errorf("cursor past the end is %d cells wide, want 3", got)
	}
}

// TestComposerAtAShortPanelKeepsOneBodyRow proves a panel too short for the three header
// fields plus a body still allocates a body row, rather than composing an empty panel.
func TestComposerAtAShortPanelKeepsOneBodyRow(t *testing.T) {
	c := newComposer(mail.Compose{Text: "body"}, false)

	rows := c.view(20, composerFields)
	if len(rows) != composerFields {
		t.Fatalf("%d rows, want the %d header rows only: %v", len(rows), composerFields, rows)
	}

	rows = c.view(20, composerFields+1)
	if len(rows) != composerFields+1 {
		t.Fatalf("%d rows, want room for one body row: %v", len(rows), rows)
	}
	if last := rows[len(rows)-1]; !strings.Contains(last, "body") {
		t.Errorf("last row = %q, want the body text", last)
	}
}

// TestComposerScrollReturnsWhenTheBodyShrinks proves the scroll offset follows the cursor
// back after the text is replaced with something shorter, instead of staying past the end
// and rendering a blank body.
func TestComposerScrollReturnsWhenTheBodyShrinks(t *testing.T) {
	lines := make([]string, 40)
	for i := range lines {
		lines[i] = "line"
	}

	c := newComposer(mail.Compose{Text: strings.Join(lines, "\n")}, true)
	c.view(40, 10)
	if c.scroll == 0 {
		t.Fatal("the composer did not scroll to the end of a long body")
	}

	c.body.setText("short")
	rows := c.view(40, 10)

	if c.scroll != 0 {
		t.Errorf("scroll = %d after the body shrank, want 0", c.scroll)
	}
	if joined := strings.Join(rows, "\n"); !strings.Contains(joined, "short") {
		t.Errorf("the body is not visible after shrinking:\n%s", joined)
	}
}
