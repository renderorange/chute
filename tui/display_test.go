package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mattn/go-runewidth"
	"github.com/renderorange/chute/mail"
)

// widthChangingRunes are characters a terminal draws with a different number of cells
// than the width tables chute and bubbletea use. U+00AD is the one that caused the
// reported frame scroll: terminals advance one cell for a soft hyphen while
// go-runewidth and x/ansi count zero, so a line padded to the frame width wraps and the
// whole view scrolls.
var widthChangingRunes = []rune{
	'\u00ad', // soft hyphen: 1 cell in a terminal, 0 here
	'\u034f', // combining grapheme joiner
	'\u200b', // zero-width space
	'\u200c', // zero-width non-joiner
	'\u200d', // zero-width joiner
	'\u2060', // word joiner
	'\ufeff', // zero-width no-break space
}

// TestWrapTextStripsWidthChangingRunes proves the reader's wrapper never hands the
// terminal a character it measures differently.
func TestWrapTextStripsWidthChangingRunes(t *testing.T) {
	in := "Please use your *$6.53 offer* ASAP" +
		string(widthChangingRunes) + "\n"

	for _, line := range wrapText(in, 20) {
		for _, r := range widthChangingRunes {
			if strings.ContainsRune(line, r) {
				t.Errorf("wrapped line %q still carries U+%04X", line, r)
			}
		}
	}
}

// TestWrapTextWrapsByDisplayCells pins the second half of the invariant: a wrapped line
// must not exceed the requested width in cells. Counting runes lets a wide-rune body
// overrun the pane, which lipgloss then re-wraps and the panel clips, losing text.
func TestWrapTextWrapsByDisplayCells(t *testing.T) {
	got := wrapText(strings.Repeat("日本語", 8), 7)

	for i, line := range got {
		if w := runewidth.StringWidth(line); w > 7 {
			t.Errorf("line %d is %d cells, want at most 7: %q", i, w, line)
		}
	}
}

// TestViewEmitsNoWidthChangingRunes is the regression test for the frame scroll: the
// rendered frame must not contain a character the terminal draws differently from the
// width functions. A body of these characters is exactly what a marketing email sends.
func TestViewEmitsNoWidthChangingRunes(t *testing.T) {
	body := &mail.Body{Text: strings.Repeat("Apply it for 12pcs 12mm Mini ... "+
		string(widthChangingRunes)+"\u00a0\u2007\n", 40)}

	m := openSelectedBody(t, loadedBody(t, sampleMessages(1), body))
	next, _ := m.Update(tea.WindowSizeMsg{Width: 274, Height: 56})
	m = next.(Model)

	view := m.View()
	for _, r := range widthChangingRunes {
		if strings.ContainsRune(view, r) {
			t.Errorf("the frame still carries U+%04X, which the terminal draws wider", r)
		}
	}
}
