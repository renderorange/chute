package tui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/renderorange/chute/mail"
)

// bodyText is n lines of prose, several times taller than any reader pane.
func bodyText(n int) string {
	var b strings.Builder
	for i := 1; i <= n; i++ {
		fmt.Fprintf(&b, "line %03d of the body, with enough words that it has to wrap somewhere\n", i)
	}

	return b.String()
}

// TestLongBodyKeepsTheFrameExact is #44 stated as a test at every size the app can be
// run at. The report is that returning from a long message shifts the list up one row
// and repeats a row; a frame of exactly the terminal's height, every line exactly the
// terminal's width, with an unchanged list window, is the property that rules it out.
//
// The sizes span all three layouts: single below 110 columns, two panes to 119, three
// from 120.
func TestLongBodyKeepsTheFrameExact(t *testing.T) {
	for _, size := range []struct{ width, height int }{
		{105, 59}, // the tablet, single pane
		{110, 40},
		{119, 30},
		{120, 40}, // first three-pane width
		{130, 40},
		{140, 50},
		{200, 60},
	} {
		t.Run(fmt.Sprintf("%dx%d", size.width, size.height), func(t *testing.T) {
			m := loaded(t, size.width, size.height, sampleFolders(3), nil)
			m.loadMessages = fakeMessages(sampleMessages(60), nil)
			m = openInboxAnyMode(t, m)

			// Scroll the list, then open a long body: the list is scrolled in the report.
			for i := 0; i < 40; i++ {
				next, _ := m.Update(namedKey("down"))
				m = next.(Model)
			}

			m.bodies[m.selectedBodyKey()] = &mail.Body{Text: bodyText(300)}
			m.focus = focusReader

			// #44's trigger: a long error in the status bar while a long body is open.
			// Before GH-23 the bar appended context and truncated the end, so it could
			// exceed the frame and wrap. The view then became one line too tall,
			// bubbletea kept the last `height` lines, and the top row was lost — which
			// reads as the list shifting up one row with a row repeated at the bottom.
			m.status = `mail: account "fastmail": dial tcp 127.0.0.1:993: ` + strings.Repeat("x", 200)

			scrollBefore := m.messageScroll
			windowBefore := listText(m)

			assertExactFrame(t, m, "reader open with a long status")

			next, _ := m.Update(namedKey("esc"))
			m = next.(Model)

			assertExactFrame(t, m, "back in the list")

			if got := m.messageScroll; got != scrollBefore {
				t.Errorf("message scroll = %d after esc, want %d", got, scrollBefore)
			}
			if after := listText(m); !equalRows(after, windowBefore) {
				t.Errorf("the list window changed after esc:\n before %q\n after  %q", windowBefore, after)
			}
		})
	}
}

// TestPanelClipsABodyWiderThanItsBox pins the second half of the property: no panel body
// can lengthen the frame, whatever the message contains. wrapText and Truncate count
// runes rather than display cells, so a long word or a wide rune can produce a line
// lipgloss then wraps; the panel must still be exactly its box.
func TestPanelClipsABodyWiderThanItsBox(t *testing.T) {
	box := Box{Width: 62, Height: 34}

	for _, tc := range []struct {
		name string
		body []string
	}{
		{"a long word", []string{strings.Repeat("x", 200)}},
		{"wide runes", []string{strings.Repeat("日本語", 30)}},
		{"wide runes followed by more", []string{strings.Repeat("日本語", 30), "after"}},
		{"many lines", strings.Split(bodyText(300), "\n")},
	} {
		rendered := Model{}.renderPanel("reader", box, tc.body, true)

		if got := lipgloss.Height(rendered); got != box.Height {
			t.Errorf("%s: %d lines, want %d", tc.name, got, box.Height)
		}
		for i, line := range strings.Split(rendered, "\n") {
			if got := lipgloss.Width(line); got != box.Width {
				t.Errorf("%s: line %d is %d cells, want %d", tc.name, i, got, box.Width)
			}
		}
	}
}

// openInboxAnyMode opens INBOX through the model's own folder path. The enter key is
// not usable here: below 120 columns the folders panel is hidden and clampFocus has
// already moved the focus to the message list, so enter would hit an empty list. The
// layout is what this test varies, not the key that opens a folder.
func openInboxAnyMode(t *testing.T, m Model) Model {
	t.Helper()

	next, cmd := m.openSelectedFolder()
	if cmd == nil {
		t.Fatal("opening INBOX returned no command")
	}
	m = next.(Model)

	next, _ = m.Update(cmd())

	return next.(Model)
}

// assertExactFrame fails unless the frame is exactly the terminal's size.
func assertExactFrame(t *testing.T, m Model, label string) {
	t.Helper()

	lines := strings.Split(m.View(), "\n")
	if len(lines) != m.height {
		t.Fatalf("%s: %d lines, want %d", label, len(lines), m.height)
	}
	for i, line := range lines {
		if got := lipgloss.Width(line); got != m.width {
			t.Fatalf("%s: line %d is %d cells, want %d", label, i, got, m.width)
		}
	}
}

// listText is the message rows the list draws, exactly as rendered.
func listText(m Model) []string {
	width := m.layout().Messages.Width - (2 * PanelPaddingHorizontal)

	return m.messageLines(m.messageRows(), width)
}

// equalRows reports whether two row slices hold the same text.
func equalRows(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}

	return true
}
