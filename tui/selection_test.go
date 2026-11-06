package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/renderorange/chute/mail"
)

// withSelection opens INBOX with n sample messages, all unread, and the list
// focused, which is the state a range selection starts from.
func withSelection(t *testing.T, n int) Model {
	t.Helper()

	m := loaded(t, 140, 40, []mail.Folder{{Name: "INBOX", Delimiter: '/'}}, nil)
	m.loadMessages = fakeMessages(sampleMessages(n), nil)

	return openInbox(t, m)
}

// TestFooterReportsTheSelectedCount is the indicator the issue asked for: the
// footer names how many messages the active range covers, singular and plural.
func TestFooterReportsTheSelectedCount(t *testing.T) {
	m := press(t, withSelection(t, 3), keyMsg("v"))

	if got := m.renderFooter(); !strings.Contains(got, "selected 1 message") {
		t.Errorf("footer = %q, want it to name one selected message", got)
	}

	m = press(t, m, namedKey("down"))
	m = press(t, m, namedKey("down"))

	got := m.renderFooter()
	if !strings.Contains(got, "selected 3 messages") {
		t.Errorf("footer = %q, want it to name three selected messages", got)
	}
	if strings.Contains(got, "selected 1 message") {
		t.Errorf("footer = %q, still claims one message", got)
	}
}

// TestFooterReportsNoCountWithoutARange proves the count is a selection
// indicator, not a permanent label: an ordinary list footer has no prefix.
func TestFooterReportsNoCountWithoutARange(t *testing.T) {
	m := withSelection(t, 3)

	if got := m.renderFooter(); strings.Contains(got, "selected") {
		t.Errorf("footer = %q, want no selection prefix before v", got)
	}
}

// TestFooterSelectionListsEveryBulkAction is #96's "all of the options": every
// action the range supports is named while the range is active, and the whole
// set still fits the tablet width.
func TestFooterSelectionListsEveryBulkAction(t *testing.T) {
	m := press(t, withSelection(t, 3), keyMsg("v"))

	footer := m.renderFooter()
	for _, want := range []string{
		"d:delete", "A:archive", "!:junk", "m:move", "f:flag", "u:read",
		"esc:cancel", "up/down:extend",
	} {
		if !strings.Contains(footer, want) {
			t.Errorf("footer is missing %q: %q", want, footer)
		}
	}

	if got, limit := lipgloss.Width(footer), ContentWidth(m.width); got > limit {
		t.Errorf("footer is %d cells, want at most %d: %q", got, limit, footer)
	}
}

// TestFooterSelectionFitsTheMinimumFrame keeps the new prefix from breaking the
// frame at the 60x20 floor: the footer is one line and never wider than the app.
func TestFooterSelectionFitsTheMinimumFrame(t *testing.T) {
	m := loaded(t, 60, 20, []mail.Folder{{Name: "INBOX", Delimiter: '/'}}, nil)
	m.loadMessages = fakeMessages(sampleMessages(3), nil)
	m = openInboxAnyMode(t, m)
	m = press(t, m, keyMsg("v"))

	assertExactFrame(t, m, "visual range at 60x20")

	if got, limit := lipgloss.Width(m.renderFooter()), ContentWidth(m.width); got > limit {
		t.Errorf("footer is %d cells, want at most %d", got, limit)
	}
}

// TestHelpListsTheSelectionAndMoveFilterOptions covers #96's help half: the
// overlay names the keys a range adds and the move picker's typing filter.
func TestHelpListsTheSelectionAndMoveFilterOptions(t *testing.T) {
	m := openHelp(t, sized(140, 80))
	view := m.View()

	for _, want := range []string{
		"selection", "extend range", "delete selected", "filter folders",
	} {
		if !strings.Contains(view, want) {
			t.Errorf("help is missing %q:\n%s", want, view)
		}
	}
}
