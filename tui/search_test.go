package tui

import (
	"errors"
	"strings"
	"testing"

	"github.com/emersion/go-imap/v2"
	"github.com/renderorange/chute/config"
	"github.com/renderorange/chute/mail"
)

// fakeSearch records server searches and returns canned results.
type fakeSearch struct {
	calls    int
	folder   string
	query    string
	messages []mail.Message
	err      error
}

func (f *fakeSearch) load(_ config.Account, folder, query string) ([]mail.Message, error) {
	f.calls++
	f.folder = folder
	f.query = query

	return f.messages, f.err
}

// withSearch opens INBOX with a recording search loader.
func withSearch(t *testing.T, messages int) (Model, *fakeSearch) {
	t.Helper()

	m := openInbox(t, withInbox(t, sampleMessages(messages), nil))

	search := &fakeSearch{}
	m.searchMessages = search.load

	return m, search
}

// typeSearch types a query into the open prompt.
func typeSearch(t *testing.T, m Model, query string) Model {
	t.Helper()

	for _, r := range query {
		m = press(t, m, key(r))
	}

	return m
}

// TestSearchOpensAPromptAndSendsTheQuery is the headline: S asks the server, with the query
// and the open folder, exactly once.
func TestSearchOpensAPromptAndSendsTheQuery(t *testing.T) {
	m, search := withSearch(t, 3)
	search.messages = sampleMessages(2)

	m = press(t, m, keyMsg("S"))

	if !m.searchOpen {
		t.Fatal("S did not open the search prompt")
	}

	m = typeSearch(t, m, "invoice")
	m = press(t, m, namedKey("enter"))

	if search.calls != 1 {
		t.Fatalf("searches = %d, want 1", search.calls)
	}
	if search.folder != "INBOX" || search.query != "invoice" {
		t.Errorf("searched %q for %q, want INBOX and invoice", search.folder, search.query)
	}
	if !m.searchActive {
		t.Fatal("the results are not showing")
	}
	if len(m.rows) != 2 {
		t.Errorf("rows = %d, want 2", len(m.rows))
	}
	for i, row := range m.rows {
		if row.Depth != 0 {
			t.Errorf("row %d depth = %d, want 0: a result has no thread context", i, row.Depth)
		}
	}
	if !strings.Contains(m.status, "found") {
		t.Errorf("status = %q, want a match count", m.status)
	}
}

// TestSearchEscapeRestoresTheFolderList proves the result list is transient: esc brings back
// the loaded folder and the cursor it had.
func TestSearchEscapeRestoresTheFolderList(t *testing.T) {
	m, search := withSearch(t, 3)
	search.messages = sampleMessages(1)

	m.messageCursor = 2
	m = press(t, m, keyMsg("S"))
	m = typeSearch(t, m, "x")
	m = press(t, m, namedKey("enter"))

	if !m.searchActive || len(m.rows) != 1 {
		t.Fatalf("search state = %v with %d rows, want the one result", m.searchActive, len(m.rows))
	}

	m = press(t, m, namedKey("esc"))

	if m.searchActive {
		t.Error("esc left the search active")
	}
	if len(m.rows) != 3 {
		t.Errorf("rows = %d, want the folder's 3 back", len(m.rows))
	}
	if m.messageCursor != 2 {
		t.Errorf("cursor = %d, want the pre-search 2", m.messageCursor)
	}
	if m.status != "" {
		t.Errorf("status = %q, want empty", m.status)
	}
}

// TestSearchEmptyQueryIsRejected proves an empty prompt sends nothing: a bare SEARCH matches
// the whole folder and would look like a reload.
func TestSearchEmptyQueryIsRejected(t *testing.T) {
	m, search := withSearch(t, 3)

	m = press(t, m, keyMsg("S"))
	m = press(t, m, namedKey("enter"))

	if search.calls != 0 {
		t.Errorf("searches = %d, want 0", search.calls)
	}
	if m.searchOpen {
		t.Error("the prompt stayed open after an empty query")
	}
	if want := "empty search"; m.status != want {
		t.Errorf("status = %q, want %q", m.status, want)
	}
}

// TestSearchWithNoMatchesKeepsTheList proves an empty result reports without replacing what
// the user was looking at.
func TestSearchWithNoMatchesKeepsTheList(t *testing.T) {
	m, search := withSearch(t, 3)
	search.messages = nil

	m = press(t, m, keyMsg("S"))
	m = typeSearch(t, m, "nothing")
	m = press(t, m, namedKey("enter"))

	if m.searchActive {
		t.Error("an empty result replaced the folder list")
	}
	if len(m.rows) != 3 {
		t.Errorf("rows = %d, want the folder's 3", len(m.rows))
	}
	if want := "no matches"; m.status != want {
		t.Errorf("status = %q, want %q", m.status, want)
	}
}

// TestSearchFailureReportsAndKeepsTheList covers the timeout case the issue names.
func TestSearchFailureReportsAndKeepsTheList(t *testing.T) {
	m, search := withSearch(t, 3)
	search.err = errors.New("mail: account \"fastmail\": searching \"INBOX\": server timed out")

	m = press(t, m, keyMsg("S"))
	m = typeSearch(t, m, "x")
	m = press(t, m, namedKey("enter"))

	if m.searchActive || len(m.rows) != 3 {
		t.Error("a failed search changed the list")
	}
	if !strings.Contains(m.status, "timed out") {
		t.Errorf("status = %q, want the failure", m.status)
	}
}

// TestSearchResultsRefuseFolderChanges is the issue's caution: the UID set may not match the
// folder view, so delete and move say so instead of acting.
func TestSearchResultsRefuseFolderChanges(t *testing.T) {
	for _, tc := range []struct {
		name string
		key  string
	}{
		{"delete", "d"},
		{"archive", "A"},
		{"junk", "!"},
		{"move", "M"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, search := withSearch(t, 3)
			search.messages = sampleMessages(1)

			remove := &fakeDelete{}
			m.deleteMessages = remove.load

			m = press(t, m, keyMsg("S"))
			m = typeSearch(t, m, "x")
			m = press(t, m, namedKey("enter"))
			m = press(t, m, keyMsg(tc.key))

			if m.confirmOpen || m.moveOpen {
				t.Error("a folder-changing action opened a dialog from the results")
			}
			if remove.calls != 0 {
				t.Errorf("delete calls = %d, want 0", remove.calls)
			}
			if want := "search results: esc to return to the folder"; m.status != want {
				t.Errorf("status = %q, want %q", m.status, want)
			}
		})
	}
}

// TestSearchNeedsAnOpenFolder avoids a prompt that can only fail.
func TestSearchNeedsAnOpenFolder(t *testing.T) {
	m := sized(140, 40)

	m = press(t, m, keyMsg("S"))

	if m.searchOpen {
		t.Error("S opened the prompt with no folder")
	}
	if want := "open a folder to search"; m.status != want {
		t.Errorf("status = %q, want %q", m.status, want)
	}
}

// TestSearchPromptFitsTheFrame pins the dialog at the minimum terminal size.
func TestSearchPromptFitsTheFrame(t *testing.T) {
	m := loaded(t, 60, 20, []mail.Folder{{Name: "INBOX", Delimiter: '/'}}, nil)
	m.loadMessages = fakeMessages(sampleMessages(2), nil)
	m = openInboxAnyMode(t, m)

	m = press(t, m, keyMsg("S"))

	assertExactFrame(t, m, "search prompt")
}

// TestSearchResultSurvivesAStaleAccount keeps another account's late result out of this list.
func TestSearchResultSurvivesAStaleAccount(t *testing.T) {
	m, _ := withSearch(t, 2)
	m.status = "untouched"

	next, _ := m.Update(searchMsg{account: "other", folder: "INBOX", messages: sampleMessages(1)})
	m = next.(Model)

	if m.searchActive {
		t.Error("a late result for another account activated the list")
	}
	if m.status != "untouched" || len(m.rows) != 2 {
		t.Errorf("status = %q, rows = %d, want untouched and 2", m.status, len(m.rows))
	}
}

// TestSearchResetWhenTheFolderChanges proves the prompt and results do not follow the user into
// another folder.
func TestSearchResetWhenTheFolderChanges(t *testing.T) {
	m, search := withSearch(t, 2)
	search.messages = sampleMessages(1)

	m = press(t, m, keyMsg("S"))
	m = typeSearch(t, m, "x")
	m = press(t, m, namedKey("enter"))

	if !m.searchActive {
		t.Fatal("the search did not activate")
	}

	next, cmd := m.openFolderByName("INBOX", false)
	m = next.(Model)
	m = deliver(t, m, cmd)

	if m.searchActive || len(m.searchResults) != 0 {
		t.Error("search results survived opening a folder")
	}
}

// TestSearchKeyCanBeRebound is the M6 property for the new action.
func TestSearchKeyCanBeRebound(t *testing.T) {
	m, _ := withSearch(t, 2)

	keys := defaultBindings()
	keys[actionSearch] = []string{"ctrl+f"}
	m.keys = keys

	m = press(t, m, keyMsg("ctrl+f"))
	if !m.searchOpen {
		t.Error("the rebound key did not open the prompt")
	}
}

// TestSearchRowsAreTheMatches guards the model contract: the reader opens the selected result
// by UID, so the rows must carry the fetched messages.
func TestSearchRowsAreTheMatches(t *testing.T) {
	m, search := withSearch(t, 3)
	search.messages = sampleMessages(2)

	m = press(t, m, keyMsg("S"))
	m = typeSearch(t, m, "x")
	m = press(t, m, namedKey("enter"))

	if len(m.rows) != len(search.messages) {
		t.Fatalf("rows = %d, want the %d results", len(m.rows), len(search.messages))
	}

	want := make(map[imap.UID]bool, len(search.messages))
	for _, message := range search.messages {
		want[message.UID] = true
	}
	for _, row := range m.rows {
		if !want[row.Message.UID] {
			t.Errorf("row UID %d is not one of the results", row.Message.UID)
		}
	}

	if selected, ok := m.selectedMessage(); !ok || !want[selected.UID] {
		t.Errorf("selected message = %v, want one of the results", selected.UID)
	}
}
