package tui

import (
	"regexp"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// boldSGR matches the bold attribute inside any SGR sequence: lipgloss packs
// bold and colour into one escape ("\x1b[1;38;5;238m"), so a literal "\x1b[1m"
// never appears.
var boldSGR = regexp.MustCompile("\x1b\\[[0-9;]*1[;m]")

// boldRows renders the list with colour forced on, so "is this row bold" is a
// real question under `go test`'s piped stdout. The profile is restored before
// the test returns; no test in this package uses t.Parallel.
func boldRows(t *testing.T, m Model) []string {
	t.Helper()

	profile := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(profile) })

	return m.messageLines(3, 60)
}

// TestOnlySelectedRowsAreBold is #97: bold marks the cursor and the active
// range, and nothing else. Every fixture message is unread, so an unread row
// outside the selection is the row that must stay plain.
func TestOnlySelectedRowsAreBold(t *testing.T) {
	m := withSelection(t, 3)

	lines := boldRows(t, m)
	if !boldSGR.MatchString(lines[0]) {
		t.Error("the cursor row is not bold")
	}
	for i := 1; i < len(lines); i++ {
		if boldSGR.MatchString(lines[i]) {
			t.Errorf("row %d is bold while only the cursor is selected", i)
		}
	}

	m = press(t, m, keyMsg("v"))
	m = press(t, m, namedKey("down"))

	lines = boldRows(t, m)
	for i := 0; i < 2; i++ {
		if !boldSGR.MatchString(lines[i]) {
			t.Errorf("row %d is in the range but not bold", i)
		}
	}
	if boldSGR.MatchString(lines[2]) {
		t.Error("an unread row outside the range is bold")
	}
}

// TestUnreadRowsKeepTheStarMarker proves removing the bold did not remove the
// unread signal itself: the marker column still carries it.
func TestUnreadRowsKeepTheStarMarker(t *testing.T) {
	m := withSelection(t, 2)

	for i, line := range m.messageLines(2, 60) {
		if i == 0 {
			continue // the cursor row shows > in the marker column
		}
		if !strings.HasPrefix(strings.TrimLeft(line, " "), "*") {
			t.Errorf("row %d lost its unread marker: %q", i, line)
		}
	}
}
