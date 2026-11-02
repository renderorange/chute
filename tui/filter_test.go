package tui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/emersion/go-imap/v2"
	"github.com/renderorange/chute/mail"
)

// filterMessage builds one message with a distinct sender, subject and recipient.
func filterMessage(uid imap.UID, day int, from, subject, to string) mail.Message {
	return mail.Message{
		UID:    uid,
		SeqNum: uint32(uid),
		Envelope: imap.Envelope{
			Subject: subject,
			From:    []imap.Address{{Name: from, Mailbox: strings.ToLower(from), Host: "example.org"}},
			To:      []imap.Address{{Name: to, Mailbox: strings.ToLower(to), Host: "example.org"}},
			Date:    time.Date(2026, 10, day, 9, 0, 0, 0, time.UTC),
		},
	}
}

// filterMessages is a folder where every field of the filter is exercised: sender,
// subject and recipient each pick out a different message.
func filterMessages() []mail.Message {
	return []mail.Message{
		filterMessage(1, 1, "Alice", "Invoice March", "Bob"),
		filterMessage(2, 2, "Bob", "Lunch on Friday", "Alice"),
		filterMessage(3, 3, "Carol", "invoices april", "Team"),
	}
}

// withFilter returns an opened INBOX holding the three distinct messages.
func withFilter(t *testing.T) Model {
	t.Helper()

	return openInbox(t, withInbox(t, filterMessages(), nil))
}

// visibleUIDs returns the UIDs the list is showing, in list order.
func visibleUIDs(m Model) []imap.UID {
	uids := make([]imap.UID, 0, len(m.rows))
	for _, row := range m.rows {
		uids = append(uids, row.Message.UID)
	}

	return uids
}

// sameUIDs reports whether got holds exactly want, in any order.
func sameUIDs(got, want []imap.UID) bool {
	if len(got) != len(want) {
		return false
	}

	seen := make(map[imap.UID]bool, len(got))
	for _, uid := range got {
		seen[uid] = true
	}
	for _, uid := range want {
		if !seen[uid] {
			return false
		}
	}

	return true
}

// typeQuery opens the prompt and types a query one rune at a time, so the live
// narrowing is exercised the way a user drives it.
func typeQuery(t *testing.T, m Model, query string) Model {
	t.Helper()

	next, _ := m.Update(keyMsg("/"))
	m = next.(Model)
	if !m.filterOpen {
		t.Fatal("/ did not open the filter prompt")
	}

	for _, r := range query {
		next, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = next.(Model)
	}

	return m
}

// applyQuery opens the prompt, types the query and presses enter.
func applyQuery(t *testing.T, m Model, query string) Model {
	t.Helper()

	m = typeQuery(t, m, query)

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})

	return next.(Model)
}

// TestFilterKeyIsBound proves / is the key the issue names.
func TestFilterKeyIsBound(t *testing.T) {
	keys := defaultBindings()[actionFilter]
	if len(keys) != 1 || keys[0] != "/" {
		t.Errorf("actionFilter is bound to %v, want /", keys)
	}
}

// TestFilterPromptNarrowsRowsAsTheQueryGrows is the issue's first criterion: the
// prompt opens, and typing narrows the list case-insensitively.
func TestFilterPromptNarrowsRowsAsTheQueryGrows(t *testing.T) {
	m := withFilter(t)

	m = typeQuery(t, m, "inv")

	if !m.filterOpen {
		t.Error("the prompt closed while typing")
	}
	if got := visibleUIDs(m); !sameUIDs(got, []imap.UID{1, 3}) {
		t.Errorf("visible uids = %v, want the two invoices", got)
	}

	view := m.View()
	for _, want := range []string{"filter", "/inv", "2 of 3 messages", "enter:keep", "esc:clear"} {
		if !strings.Contains(view, want) {
			t.Errorf("the prompt is missing %q:\n%s", want, view)
		}
	}
}

// TestFilterMatchesSenderSubjectAndRecipient proves all three fields are searched.
func TestFilterMatchesSenderSubjectAndRecipient(t *testing.T) {
	cases := []struct {
		query string
		want  []imap.UID
	}{
		{"alice", []imap.UID{1, 2}}, // sender of 1, recipient of 2
		{"lunch", []imap.UID{2}},    // subject only
		{"carol", []imap.UID{3}},    // sender only
		{"team", []imap.UID{3}},     // recipient only
	}

	for _, c := range cases {
		m := applyQuery(t, withFilter(t), c.query)

		if got := visibleUIDs(m); !sameUIDs(got, c.want) {
			t.Errorf("query %q kept %v, want %v", c.query, got, c.want)
		}
	}
}

// TestFilterIsCaseInsensitive is the issue's "case-insensitively" criterion. The
// two invoice subjects differ in case, so one query has to find both.
func TestFilterIsCaseInsensitive(t *testing.T) {
	m := applyQuery(t, withFilter(t), "INVOICE")

	if got := visibleUIDs(m); !sameUIDs(got, []imap.UID{1, 3}) {
		t.Errorf("uppercase query kept %v, want the same rows as lowercase", got)
	}
}

// TestFilterIsFuzzy is the paradigm the owner chose: a subsequence matches, so
// "ivm" finds "Invoice March".
func TestFilterIsFuzzy(t *testing.T) {
	m := applyQuery(t, withFilter(t), "ivm")

	if got := visibleUIDs(m); !sameUIDs(got, []imap.UID{1}) {
		t.Errorf("fuzzy query kept %v, want the one message whose subject contains the subsequence", got)
	}
}

// TestFilterKeepsTheAncestorsOfAMatchingReply is the issue's threading criterion: a
// matching reply brings its parent, so the indent is not a lie.
func TestFilterKeepsTheAncestorsOfAMatchingReply(t *testing.T) {
	parent := threadMessage(1, "parent subject", "<p>", 1)
	reply := threadMessage(2, "reply subject", "<r>", 2, "<p>")
	other := threadMessage(3, "unrelated subject", "<o>", 3)

	m := openInbox(t, withInbox(t, []mail.Message{parent, reply, other}, nil))
	m = applyQuery(t, m, "reply")

	if got := visibleUIDs(m); !sameUIDs(got, []imap.UID{1, 2}) {
		t.Fatalf("visible uids = %v, want the reply and its parent", got)
	}

	depths := map[imap.UID]int{}
	for _, row := range m.rows {
		depths[row.Message.UID] = row.Depth
	}
	if depths[1] != 0 || depths[2] != 1 {
		t.Errorf("depths = %v, want the parent at 0 and the reply at 1", depths)
	}
}

// TestFilterHidesOtherConversations proves the filter is not "keep everything once
// one thread matches".
func TestFilterHidesOtherConversations(t *testing.T) {
	aParent := threadMessage(1, "alpha one", "<a1>", 4)
	aReply := threadMessage(2, "alpha two", "<a2>", 5, "<a1>")
	bParent := threadMessage(3, "beta one", "<b1>", 1)

	m := openInbox(t, withInbox(t, []mail.Message{aParent, aReply, bParent}, nil))
	m = applyQuery(t, m, "alpha")

	if got := visibleUIDs(m); !sameUIDs(got, []imap.UID{1, 2}) {
		t.Errorf("visible uids = %v, want only the alpha conversation", got)
	}
}

// TestFilterEnterKeepsTheRowsAndNavigates proves enter keeps the filter, closes the
// prompt, and leaves the arrows working over the filtered rows.
func TestFilterEnterKeepsTheRowsAndNavigates(t *testing.T) {
	m := applyQuery(t, withFilter(t), "inv")

	if m.filterOpen {
		t.Fatal("enter did not close the prompt")
	}
	if m.filter != "inv" {
		t.Errorf("filter = %q, want it kept", m.filter)
	}
	if got := len(m.rows); got != 2 {
		t.Fatalf("rows = %d, want the two matches", got)
	}
	if got := visibleUIDs(m)[m.messageCursor]; got != m.rows[m.messageCursor].Message.UID {
		t.Fatalf("cursor is outside the filtered rows")
	}

	next, _ := m.Update(namedKey("down"))
	m = next.(Model)

	if m.messageCursor != 1 {
		t.Errorf("cursor = %d, want the second of two filtered rows", m.messageCursor)
	}
}

// TestFilterEscapeClearsWhileTyping proves esc inside the prompt restores every row.
func TestFilterEscapeClearsWhileTyping(t *testing.T) {
	m := typeQuery(t, withFilter(t), "inv")

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = next.(Model)

	if m.filterOpen {
		t.Error("esc did not close the prompt")
	}
	if m.filter != "" {
		t.Errorf("filter = %q, want it cleared", m.filter)
	}
	if got := len(m.rows); got != 3 {
		t.Errorf("rows = %d, want every row back", got)
	}
}

// TestFilterEscapeOnTheListClears proves the owner's ruling: after enter, esc on the
// message list clears the filter.
func TestFilterEscapeOnTheListClears(t *testing.T) {
	m := applyQuery(t, withFilter(t), "inv")
	if m.focus != focusMessages {
		t.Fatalf("focus = %v, want the message list", m.focus)
	}

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = next.(Model)

	if m.filter != "" {
		t.Errorf("filter = %q, want esc to clear it", m.filter)
	}
	if got := len(m.rows); got != 3 {
		t.Errorf("rows = %d, want every row back", got)
	}
}

// TestFilterEscapeInTheReaderIsStillBack proves clearing did not steal esc from the
// reading pane.
func TestFilterEscapeInTheReaderIsStillBack(t *testing.T) {
	m := applyQuery(t, withFilter(t), "inv")
	m.focus = focusReader

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = next.(Model)

	if m.focus != focusMessages {
		t.Errorf("focus = %v, want esc to leave the reader", m.focus)
	}
	if m.filter != "inv" {
		t.Errorf("filter = %q, want the filter kept while leaving the reader", m.filter)
	}
}

// TestFilterTypingDoesNotTriggerActions proves the prompt swallows the keymap: q
// types a letter instead of quitting, and d does not delete.
func TestFilterTypingDoesNotTriggerActions(t *testing.T) {
	m := withFilter(t)

	next, _ := m.Update(keyMsg("/"))
	m = next.(Model)
	next, cmd := m.Update(keyMsg("q"))
	m = next.(Model)

	if cmd != nil {
		t.Error("q while typing returned a command, so it reached the keymap")
	}
	if m.filter != "q" {
		t.Errorf("filter = %q, want the typed letter", m.filter)
	}
	if !m.filterOpen {
		t.Error("typing q closed the prompt")
	}

	next, _ = m.Update(keyMsg("d"))
	m = next.(Model)
	if m.confirmOpen {
		t.Error("d while typing opened the delete confirmation")
	}
	if m.filter != "qd" {
		t.Errorf("filter = %q, want both letters", m.filter)
	}
}

// TestFilterSpaceAndBackspace proves the prompt edits like a text field.
func TestFilterSpaceAndBackspace(t *testing.T) {
	m := typeQuery(t, withFilter(t), "lunch fri")

	if m.filter != "lunch fri" {
		t.Errorf("filter = %q, want the space kept", m.filter)
	}

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	m = next.(Model)

	if m.filter != "lunch fr" {
		t.Errorf("filter = %q, want the last rune removed", m.filter)
	}
}

// TestFilterTrimsAQueryOfOnlySpaces proves a space-only query commits as no filter
// rather than as a filter that hides nothing but advertises itself.
func TestFilterTrimsAQueryOfOnlySpaces(t *testing.T) {
	m := applyQuery(t, withFilter(t), "   ")

	if m.filter != "" {
		t.Errorf("filter = %q, want the spaces dropped", m.filter)
	}
	if got := len(m.rows); got != 3 {
		t.Errorf("rows = %d, want every row", got)
	}
	if strings.Contains(m.renderFooter(), "filter:") {
		t.Error("the footer advertises a filter that does nothing")
	}
}

// TestFilterPromptFitsANarrowTerminal proves a long query cannot widen the frame.
// Every line of every view in this app is exactly the terminal width, so the one
// line a user types into needs a cap like any other.
func TestFilterPromptFitsANarrowTerminal(t *testing.T) {
	m := withFilter(t)

	next, _ := m.Update(tea.WindowSizeMsg{Width: 60, Height: 30})
	m = next.(Model)
	m = typeQuery(t, m, strings.Repeat("x", 80))

	for i, line := range strings.Split(m.View(), "\n") {
		if got := lipgloss.Width(line); got != 60 {
			t.Fatalf("line %d is %d cells, want 60: %q", i, got, line)
		}
	}
}

// TestFilterNoMatchesShowsAHint covers the issue's empty-result criterion: no rows,
// and a hint rather than "no messages".
func TestFilterNoMatchesShowsAHint(t *testing.T) {
	m := typeQuery(t, withFilter(t), "zzz")

	if got := len(m.rows); got != 0 {
		t.Fatalf("rows = %d, want none", got)
	}
	if !strings.Contains(m.View(), "no matches") {
		t.Errorf("the prompt does not say there are no matches:\n%s", m.View())
	}

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)

	view := m.View()
	if !strings.Contains(view, `no messages match "zzz"`) {
		t.Errorf("the list does not name the empty result:\n%s", view)
	}
	if strings.Contains(view, "| no messages") {
		t.Error("the list still claims the folder is empty")
	}
}

// TestFilterClearsOnFolderChange proves a filter does not leak into another mailbox.
func TestFilterClearsOnFolderChange(t *testing.T) {
	m := openInbox(t, withInbox(t, filterMessages(), nil))
	m = applyQuery(t, m, "inv")

	m.loadFolders = fakeFolders([]mail.Folder{{Name: "INBOX"}, {Name: "Archive"}}, nil)
	m.folders = []mail.Folder{{Name: "INBOX"}, {Name: "Archive"}}
	m.focus = focusFolders
	m.folderCursor = 1

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)

	if m.filter != "" {
		t.Errorf("filter = %q, want it cleared when the folder changes", m.filter)
	}
}

// TestFilterClearsOnAccountSwitch proves the same across accounts.
func TestFilterClearsOnAccountSwitch(t *testing.T) {
	m := openInbox(t, withInbox(t, filterMessages(), nil))
	m = applyQuery(t, m, "inv")

	m = openSelector(t, m)
	m.accountCursor = 1

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)

	if m.filter != "" {
		t.Errorf("filter = %q, want it cleared when the account changes", m.filter)
	}
}

// TestFilterClearsOnReload proves ctrl+r does not resurrect a filter over a folder
// that is about to be fetched again.
func TestFilterClearsOnReload(t *testing.T) {
	m := openInbox(t, withInbox(t, filterMessages(), nil))
	m = applyQuery(t, m, "inv")

	next, _ := m.Update(keyMsg("ctrl+r"))
	m = next.(Model)

	if m.filter != "" {
		t.Errorf("filter = %q, want it cleared on reload", m.filter)
	}
}

// TestFilterFooterShowsTheQueryAndClearHint is the issue's footer criterion.
func TestFilterFooterShowsTheQueryAndClearHint(t *testing.T) {
	m := applyQuery(t, withFilter(t), "inv")
	footer := m.renderFooter()

	for _, want := range []string{"filter:inv", "esc:clear"} {
		if !strings.Contains(footer, want) {
			t.Errorf("footer = %q, want %q", footer, want)
		}
	}

	// Without a filter the hint would advertise a key that does nothing.
	if strings.Contains(withFilter(t).renderFooter(), "esc:clear") {
		t.Error("the clear hint is shown without a filter")
	}
}

// TestFilterTriageUpdatesTheRowInPlace is the issue's triage criterion: a filtered
// row takes a flag change without the filter dropping it or the cursor moving.
func TestFilterTriageUpdatesTheRowInPlace(t *testing.T) {
	m, store := withTriage(t, filterMessages())
	m = applyQuery(t, m, "inv")
	if len(m.rows) != 2 {
		t.Fatalf("rows = %d, want the two matches", len(m.rows))
	}

	next, _ := m.Update(namedKey("down"))
	m = next.(Model)

	keep := m.rows[m.messageCursor].Message.UID

	next, cmd := m.Update(keyMsg("f"))
	m = next.(Model)
	m = deliver(t, m, cmd)

	if store.calls != 1 {
		t.Fatalf("store calls = %d, want 1", store.calls)
	}
	if len(m.rows) != 2 {
		t.Errorf("rows = %d, want the filtered list unchanged", len(m.rows))
	}
	if got := m.rows[m.messageCursor].Message.UID; got != keep {
		t.Errorf("cursor moved to UID %d, want to stay on %d", got, keep)
	}
	if !m.rows[m.messageCursor].Message.Flagged() {
		t.Error("the filtered row was not flagged")
	}
}

// TestFilterSurvivesAPollRefresh proves the filter is a view over the list, not a
// property of the messages: a refetch of the same folder keeps it.
func TestFilterSurvivesAPollRefresh(t *testing.T) {
	m := applyQuery(t, withFilter(t), "inv")

	next, _ := m.Update(messagesMsg{account: m.accountName(), folder: "INBOX", messages: filterMessages()})
	m = next.(Model)

	if m.filter != "inv" {
		t.Errorf("filter = %q, want it kept across a refresh", m.filter)
	}
	if got := len(m.rows); got != 2 {
		t.Errorf("rows = %d, want the refresh filtered too", got)
	}
}
