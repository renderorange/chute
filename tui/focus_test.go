package tui

import (
	"strings"
	"testing"

	"github.com/renderorange/chute/config"
)

// focusRings is the panel order tab walks in each layout mode, oldest first. The
// two-column ring leaves folders out because that panel is an overlay there.
var focusRings = []struct {
	name  string
	width int
	ring  []panelFocus
}{
	{name: "three", width: 140, ring: []panelFocus{focusFolders, focusMessages, focusReader}},
	{name: "two", width: 110, ring: []panelFocus{focusMessages, focusReader}},
	{name: "single", width: 50, ring: []panelFocus{focusFolders, focusMessages, focusReader}},
}

// TestShiftTabCyclesFocusBackwardsInThreeColumnMode is the issue's request: the
// reverse of tab in the layout the owner uses at 120+ columns.
func TestShiftTabCyclesFocusBackwardsInThreeColumnMode(t *testing.T) {
	m := sized(140, 40)

	if m.focus != focusFolders {
		t.Fatalf("initial focus = %v, want folders", m.focus)
	}

	next, _ := m.Update(namedKey("shift+tab"))
	if got := next.(Model).focus; got != focusReader {
		t.Errorf("focus after one shift+tab = %v, want reader", got)
	}

	next, _ = next.(Model).Update(namedKey("shift+tab"))
	if got := next.(Model).focus; got != focusMessages {
		t.Errorf("focus after two shift+tabs = %v, want messages", got)
	}

	next, _ = next.(Model).Update(namedKey("shift+tab"))
	if got := next.(Model).focus; got != focusFolders {
		t.Errorf("focus after three shift+tabs = %v, want folders", got)
	}
}

// TestShiftTabCyclesFocusBackwardsInSingleMode proves the single-panel tab cycle
// walks backwards too, without naming each step twice.
func TestShiftTabCyclesFocusBackwardsInSingleMode(t *testing.T) {
	m := sized(50, 20)

	if m.focus != focusFolders {
		t.Fatalf("initial focus = %v, want folders", m.focus)
	}

	for _, want := range []panelFocus{focusReader, focusMessages, focusFolders} {
		next, _ := m.Update(namedKey("shift+tab"))
		m = next.(Model)

		if m.focus != want {
			t.Fatalf("focus = %v, want %v", m.focus, want)
		}
	}
}

// TestTabAndShiftTabAreInverses is the property the two directions have to hold:
// prev undoes next from every panel on the ring, in every mode. Two separate
// successor tables would pass the step tests above and still drift; walking the
// same ring the other way is what this pins.
func TestTabAndShiftTabAreInverses(t *testing.T) {
	for _, tc := range focusRings {
		for _, start := range tc.ring {
			m := sized(tc.width, 30)
			m.focus = start

			forward := m.nextFocus()
			m.focus = forward
			if got := m.prevFocus(); got != start {
				t.Errorf("%s: prev(next(%v)) = %v", tc.name, start, got)
			}

			m.focus = start
			backward := m.prevFocus()
			m.focus = backward
			if got := m.nextFocus(); got != start {
				t.Errorf("%s: next(prev(%v)) = %v", tc.name, start, got)
			}
		}
	}
}

// TestShiftTabSkipsFoldersInTwoColumnMode proves the two-pane ring is honest: at
// 110 columns folders is an overlay, so both directions only alternate the two
// panels that are actually drawn.
func TestShiftTabSkipsFoldersInTwoColumnMode(t *testing.T) {
	m := sized(110, 30)

	if m.focus != focusMessages {
		t.Fatalf("initial focus = %v, want messages after clamping", m.focus)
	}

	next, _ := m.Update(namedKey("shift+tab"))
	if got := next.(Model).focus; got != focusReader {
		t.Errorf("focus after one shift+tab = %v, want reader", got)
	}

	next, _ = next.(Model).Update(namedKey("shift+tab"))
	if got := next.(Model).focus; got != focusMessages {
		t.Errorf("focus after two shift+tabs = %v, want messages", got)
	}
}

// TestShiftTabIsRebindable proves prev_panel is an ordinary [keys] entry: the
// override moves the behaviour, the replaced key goes inert and help follows.
func TestShiftTabIsRebindable(t *testing.T) {
	m := inboxWithKeys(t, map[string][]string{config.KeyPrevPanel: {"ctrl+p"}})
	m.focus = focusFolders

	next, _ := m.Update(namedKey("ctrl+p"))
	if got := next.(Model).focus; got != focusReader {
		t.Errorf("focus after the rebound key = %v, want reader", got)
	}

	next, _ = m.Update(namedKey("shift+tab"))
	if got := next.(Model).focus; got != focusFolders {
		t.Errorf("focus after the replaced key = %v, want folders left alone", got)
	}

	help := openHelp(t, m).View()
	if !strings.Contains(help, "ctrl+p") {
		t.Errorf("help does not show the rebound key:\n%s", help)
	}
}
