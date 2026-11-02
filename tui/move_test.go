package tui

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/emersion/go-imap/v2"
	"github.com/renderorange/chute/config"
	"github.com/renderorange/chute/mail"
)

// fakeMove records archive and junk calls so tests can prove the guard, the role and
// the UID set without a server.
type fakeMove struct {
	calls  int
	folder string
	role   mail.Role
	uids   []imap.UID
	dest   string
	err    error
}

// load stands in for the session-bound moveToRole loader. On success it names the
// destination the way the session does; on failure it keeps any dest the test set.
func (f *fakeMove) load(_ config.Account, folder string, role mail.Role, uids []imap.UID) (string, error) {
	f.calls++
	f.folder = folder
	f.role = role
	f.uids = append([]imap.UID(nil), uids...)

	if f.err != nil {
		return f.dest, f.err
	}
	if f.dest == "" {
		f.dest = map[mail.Role]string{mail.RoleArchive: "Archive", mail.RoleJunk: "Junk"}[role]
	}

	return f.dest, nil
}

// withMove returns an opened INBOX wired to a recording move loader.
func withMove(t *testing.T, messages []mail.Message) (Model, *fakeMove) {
	t.Helper()

	move := &fakeMove{}
	m := openInbox(t, withInbox(t, messages, nil))
	m.moveToRole = move.load

	return m, move
}

// TestArchiveMovesTheCursorRow covers the issue's headline: A files the selection and
// the row leaves the list in place, with no refetch and no confirmation.
func TestArchiveMovesTheCursorRow(t *testing.T) {
	m, move := withMove(t, sampleMessages(3))

	next, cmd := m.Update(keyMsg("A"))
	m = next.(Model)

	if m.confirmOpen {
		t.Error("archive opened a confirmation")
	}
	if cmd == nil {
		t.Fatal("A returned no command")
	}
	if move.calls != 0 {
		t.Fatal("A reached the server before the command ran")
	}

	m = deliver(t, m, cmd)

	if move.calls != 1 {
		t.Fatalf("move calls = %d, want 1", move.calls)
	}
	if move.folder != "INBOX" {
		t.Errorf("folder = %q, want INBOX", move.folder)
	}
	if move.role != mail.RoleArchive {
		t.Errorf("role = %q, want archive", move.role)
	}
	if len(move.uids) != 1 {
		t.Errorf("uids = %v, want one", move.uids)
	}
	if len(m.messages) != 2 {
		t.Errorf("messages = %d, want 2 left", len(m.messages))
	}
	if !strings.Contains(m.status, "moved 1 message to Archive") {
		t.Errorf("status = %q, want a move confirmation naming Archive", m.status)
	}
}

// TestJunkMovesTheCursorRow proves ! uses the junk role and names the destination.
func TestJunkMovesTheCursorRow(t *testing.T) {
	m, move := withMove(t, sampleMessages(2))

	next, cmd := m.Update(keyMsg("!"))
	m = next.(Model)
	m = deliver(t, m, cmd)

	if move.calls != 1 || move.role != mail.RoleJunk {
		t.Fatalf("calls = %d, role = %q, want one junk move", move.calls, move.role)
	}
	if len(m.messages) != 1 {
		t.Errorf("messages = %d, want 1 left", len(m.messages))
	}
	if !strings.Contains(m.status, "moved 1 message to Junk") {
		t.Errorf("status = %q, want a move confirmation naming Junk", m.status)
	}
}

// TestArchiveRangeSendsOneUIDSet is the issue's "one UID set, not one command per
// message": a visual range of three is one call with three UIDs.
func TestArchiveRangeSendsOneUIDSet(t *testing.T) {
	m, move := withMove(t, sampleMessages(5))

	next, _ := m.Update(keyMsg("v"))
	m = next.(Model)
	for i := 0; i < 2; i++ {
		next, _ = m.Update(namedKey("down"))
		m = next.(Model)
	}

	next, cmd := m.Update(keyMsg("A"))
	m = next.(Model)
	m = deliver(t, m, cmd)

	if move.calls != 1 {
		t.Errorf("move calls = %d, want one command for the range", move.calls)
	}
	if len(move.uids) != 3 {
		t.Errorf("uids = %v, want three", move.uids)
	}
	if len(m.messages) != 2 {
		t.Errorf("messages = %d, want 2 left", len(m.messages))
	}
	if m.visualActive() {
		t.Error("the range stayed active after the move")
	}
}

// TestMoveFailureKeepsRows proves a rejected move leaves the list whole and reports the
// server's reason.
func TestMoveFailureKeepsRows(t *testing.T) {
	m, move := withMove(t, sampleMessages(2))
	move.err = errors.New("server said no")

	next, cmd := m.Update(keyMsg("A"))
	m = next.(Model)
	m = deliver(t, m, cmd)

	if len(m.messages) != 2 {
		t.Errorf("messages = %d, want the list unchanged", len(m.messages))
	}
	if !strings.Contains(m.status, "server said no") {
		t.Errorf("status = %q, want the server error", m.status)
	}
}

// TestMoveWithoutAFolderReports covers the issue's "no archive folder configured":
// short and specific, not the wrapped account error.
func TestMoveWithoutAFolderReports(t *testing.T) {
	m, move := withMove(t, sampleMessages(2))
	move.err = fmt.Errorf("%w: account %q", mail.ErrNoRoleFolder, "fastmail")

	next, cmd := m.Update(keyMsg("A"))
	m = next.(Model)
	m = deliver(t, m, cmd)

	if m.status != "no archive folder configured" {
		t.Errorf("archive status = %q", m.status)
	}

	m, move = withMove(t, sampleMessages(2))
	move.err = fmt.Errorf("%w: account %q", mail.ErrNoRoleFolder, "fastmail")

	next, cmd = m.Update(keyMsg("!"))
	m = next.(Model)
	m = deliver(t, m, cmd)

	if m.status != "no junk folder configured" {
		t.Errorf("junk status = %q", m.status)
	}
}

// TestMoveAlreadyInTheFolderReports proves the typed refusal names the destination
// instead of echoing the wrapped account context.
func TestMoveAlreadyInTheFolderReports(t *testing.T) {
	m, move := withMove(t, sampleMessages(1))
	move.dest = "Archive"
	move.err = fmt.Errorf("%w: account %q", mail.ErrAlreadyInRoleFolder, "fastmail")

	next, cmd := m.Update(keyMsg("A"))
	m = next.(Model)
	m = deliver(t, m, cmd)

	if m.status != "already in Archive" {
		t.Errorf("status = %q, want already in Archive", m.status)
	}
	if len(m.messages) != 1 {
		t.Errorf("messages = %d, want the message kept", len(m.messages))
	}
}

// TestMoveOnEmptyFolderReports proves the key reports instead of sending an empty set.
func TestMoveOnEmptyFolderReports(t *testing.T) {
	m, move := withMove(t, nil)

	for _, key := range []string{"A", "!"} {
		next, cmd := m.Update(keyMsg(key))
		m = next.(Model)

		if cmd != nil || move.calls != 0 {
			t.Errorf("%q sent a move with no messages", key)
		}
		if m.status == "" {
			t.Errorf("%q set no status for an empty selection", key)
		}
	}
}

// TestMoveKeysIgnoreTheFolderPanel proves archive and junk follow the same triage guard
// as delete: the folder list owns the selection and no message may be moved from it.
func TestMoveKeysIgnoreTheFolderPanel(t *testing.T) {
	m, move := withMove(t, sampleMessages(2))
	m.focus = focusFolders

	for _, key := range []string{"A", "!"} {
		next, cmd := m.Update(keyMsg(key))
		m = next.(Model)

		if cmd != nil || move.calls != 0 {
			t.Errorf("%q reached the server from the folder panel", key)
		}
	}
}

// TestMoveKeysWorkFromTheReader proves the reader keeps the keys its footer's triage
// set promises, the same rule as delete after GH-37.
func TestMoveKeysWorkFromTheReader(t *testing.T) {
	m, move := withMove(t, sampleMessages(2))
	m.focus = focusReader

	next, cmd := m.Update(keyMsg("A"))
	m = next.(Model)
	m = deliver(t, m, cmd)

	if move.calls != 1 {
		t.Errorf("move calls = %d, want the reader to archive", move.calls)
	}
}
