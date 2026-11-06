package tui

import (
	"strings"
	"testing"

	"github.com/renderorange/chute/mail"
)

// visibleFolders lists the destinations the move picker offers right now.
func visibleFolders(m Model) []string {
	names := make([]string, 0, len(m.moveMatches))
	for _, folder := range m.moveMatches {
		names = append(names, folder.Name)
	}

	return names
}

// TestMovePickerFiltersByTypedQuery is the headline of #95: typing narrows the
// folder list, and enter moves to the folder that survived the filter.
func TestMovePickerFiltersByTypedQuery(t *testing.T) {
	m, move := withFolderMove(t, 3)

	m = press(t, m, keyMsg("m"))
	m = press(t, m, keyMsg("p"))
	m = press(t, m, keyMsg("r"))

	if got := visibleFolders(m); len(got) != 1 || got[0] != "Projects" {
		t.Fatalf("picker = %v, want only Projects", got)
	}

	m = press(t, m, namedKey("enter"))

	if move.calls != 1 || move.dest != "Projects" {
		t.Errorf("moved %d times to %q, want once to Projects", move.calls, move.dest)
	}
	if m.moveOpen {
		t.Error("the picker stayed open after moving")
	}
}

// TestMovePickerFuzzyMatchesTheMiddleOfTheName proves the match is fuzzy, not a
// prefix search, so a two-letter query finds a folder whose letters are scattered.
func TestMovePickerFuzzyMatchesTheMiddleOfTheName(t *testing.T) {
	m := loaded(t, 140, 40, []mail.Folder{
		{Name: "INBOX", Delimiter: '/'},
		{Name: "Archive", Delimiter: '/'},
		{Name: "Projects", Delimiter: '/'},
		{Name: "Sent", Delimiter: '/'},
	}, nil)
	m.loadMessages = fakeMessages(sampleMessages(3), nil)
	m = openInbox(t, m)

	m = press(t, m, keyMsg("m"))
	m = press(t, m, keyMsg("a"))
	m = press(t, m, keyMsg("c"))
	m = press(t, m, keyMsg("h"))

	if got := visibleFolders(m); len(got) != 1 || got[0] != "Archive" {
		t.Errorf("picker = %v, want only Archive", got)
	}
}

// TestMovePickerSaysWhenNothingMatches keeps an empty picker honest: enter does
// nothing, the picker stays open, and the status says why.
func TestMovePickerSaysWhenNothingMatches(t *testing.T) {
	m, move := withFolderMove(t, 3)

	m = press(t, m, keyMsg("m"))
	for _, r := range []string{"z", "z", "z"} {
		m = press(t, m, keyMsg(r))
	}

	if got := visibleFolders(m); len(got) != 0 {
		t.Fatalf("picker = %v, want no matches", got)
	}

	m = press(t, m, namedKey("enter"))

	if move.calls != 0 {
		t.Errorf("loader calls = %d, want 0 with no match", move.calls)
	}
	if !m.moveOpen {
		t.Error("enter closed the picker with nothing to choose")
	}
	if want := "no folders match"; m.status != want {
		t.Errorf("status = %q, want %q", m.status, want)
	}
}

// TestMovePickerBackspaceWidensTheList proves the query is editable in place.
func TestMovePickerBackspaceWidensTheList(t *testing.T) {
	m, _ := withFolderMove(t, 3)

	m = press(t, m, keyMsg("m"))
	m = press(t, m, keyMsg("p"))
	m = press(t, m, keyMsg("r"))

	if got := len(visibleFolders(m)); got != 1 {
		t.Fatalf("visible = %d, want 1 after typing", got)
	}

	m = press(t, m, namedKey("backspace"))
	m = press(t, m, namedKey("backspace"))

	if got := len(visibleFolders(m)); got != 2 {
		t.Errorf("visible = %d, want both targets after erasing the query", got)
	}
}

// TestMovePickerLettersTypeInsteadOfClosing records the deliberate change: every
// printable key belongs to the query, so q filters rather than closing. esc is
// still the way out.
func TestMovePickerLettersTypeInsteadOfClosing(t *testing.T) {
	m, _ := withFolderMove(t, 3)

	m = press(t, m, keyMsg("m"))
	m = press(t, m, keyMsg("q"))

	if !m.moveOpen {
		t.Fatal("q closed the picker instead of typing into the query")
	}
	if m.moveFilter != "q" {
		t.Errorf("query = %q, want q", m.moveFilter)
	}

	m = press(t, m, namedKey("esc"))

	if m.moveOpen {
		t.Error("esc left the picker open")
	}
	if m.moveFilter != "" {
		t.Errorf("query = %q after closing, want empty", m.moveFilter)
	}
}

// TestMovePickerFilterFitsTheFrame pins the geometry with a query typed in: a
// typed string has no natural bound, so the dialog must clip it.
func TestMovePickerFilterFitsTheFrame(t *testing.T) {
	m := loaded(t, 60, 20, moveFolders(), nil)
	m.loadMessages = fakeMessages(sampleMessages(3), nil)
	m = openInboxAnyMode(t, m)

	m = press(t, m, keyMsg("m"))
	for _, r := range strings.Repeat("long-query", 10) {
		m = press(t, m, keyMsg(string(r)))
	}

	assertExactFrame(t, m, "move picker with a long query")
}
