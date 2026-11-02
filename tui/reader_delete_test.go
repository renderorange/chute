package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/emersion/go-imap/v2"
	"github.com/renderorange/chute/config"
	"github.com/renderorange/chute/mail"
)

// uidBody returns a loader whose body text names the message, so a test can tell
// which message the reading pane is showing rather than merely that a body exists.
func uidBody() loadBodyFunc {
	return func(_ config.Account, _ string, uid imap.UID) (*mail.Body, error) {
		return &mail.Body{Text: fmt.Sprintf("body of %d", uid)}, nil
	}
}

// runToEnd runs a command and then every command the model returns from the
// messages that follow. A delete in the reading pane answers with a body fetch,
// so a single deliver would stop half way through the behaviour under test.
func runToEnd(t *testing.T, m Model, cmd tea.Cmd) Model {
	t.Helper()

	for i := 0; i < 4 && cmd != nil; i++ {
		var next tea.Model
		next, cmd = m.Update(cmd())
		m = next.(Model)
	}
	if cmd != nil {
		t.Fatal("command chain did not settle")
	}

	return m
}

// confirmDeleteInReader presses d and then enter on the listing pane, returning
// the model after the whole chain has played out.
func confirmDeleteInReader(t *testing.T, m Model) Model {
	t.Helper()

	next, _ := m.Update(keyMsg("d"))
	m = next.(Model)

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)

	return runToEnd(t, m, cmd)
}

// TestReaderDeleteAsksBeforeActing proves d in the reading pane is gated the same
// way as d in the list: nothing reaches the server until the question is answered.
func TestReaderDeleteAsksBeforeActing(t *testing.T) {
	m, remove := withDelete(t, sampleMessages(3))
	m.loadBody = uidBody()
	m = openSelectedBody(t, m)

	if m.focus != focusReader {
		t.Fatalf("focus = %v, want the reading pane", m.focus)
	}

	next, cmd := m.Update(keyMsg("d"))
	m = next.(Model)

	if !m.confirmOpen {
		t.Fatal("d in the reading pane did not open the confirmation")
	}
	if cmd != nil {
		t.Error("d returned a command before the confirmation was answered")
	}
	if remove.calls != 0 {
		t.Error("d reached the server before the confirmation")
	}
	if !strings.Contains(m.View(), "move 1 message to trash?") {
		t.Error("the confirmation does not describe the action")
	}
}

// TestReaderDeleteAdvancesToTheNeighbour proves the reading pane follows the
// message that took the deleted row's place, and loads its body: deleting must
// not leave the reader on an empty selection.
func TestReaderDeleteAdvancesToTheNeighbour(t *testing.T) {
	m, remove := withDelete(t, sampleMessages(3))
	m.loadBody = uidBody()
	m = openSelectedBody(t, m)

	// Rows are newest-first, so the reading pane is showing UID 3.
	if got := m.rows[m.messageCursor].Message.UID; got != 3 {
		t.Fatalf("setup: cursor is on UID %d, want 3", got)
	}

	m = confirmDeleteInReader(t, m)

	if remove.calls != 1 {
		t.Fatalf("delete calls = %d, want 1", remove.calls)
	}
	if len(remove.uids) != 1 || remove.uids[0] != 3 {
		t.Errorf("uids = %v, want the message being read", remove.uids)
	}
	if len(m.messages) != 2 {
		t.Fatalf("messages = %d, want 2 left", len(m.messages))
	}
	if m.focus != focusReader {
		t.Errorf("focus = %v, want the reading pane to stay open", m.focus)
	}
	if got := m.rows[m.messageCursor].Message.UID; got != 2 {
		t.Errorf("cursor is on UID %d, want the neighbour 2", got)
	}
	if !strings.Contains(m.status, "moved 1 message to trash") {
		t.Errorf("status = %q, want the move confirmation to survive the follow-up load", m.status)
	}

	key := bodyKey{account: m.accountName(), folder: "INBOX", uid: 2}
	if _, ok := m.bodies[key]; !ok {
		t.Fatal("the neighbour's body was not fetched, so the reader is blank")
	}
	if !strings.Contains(m.View(), "body of 2") {
		t.Error("the reading pane does not show the neighbour's body")
	}
}

// TestReaderDeleteLastMessageReturnsToList proves deleting the only message
// hands focus back to the list instead of leaving the reader pointing at nothing.
func TestReaderDeleteLastMessageReturnsToList(t *testing.T) {
	m, _ := withDelete(t, sampleMessages(1))
	m.loadBody = uidBody()
	m = openSelectedBody(t, m)

	m = confirmDeleteInReader(t, m)

	if len(m.messages) != 0 {
		t.Fatalf("messages = %d, want the folder empty", len(m.messages))
	}
	if m.focus != focusMessages {
		t.Errorf("focus = %v, want the message list after the last delete", m.focus)
	}
	if !strings.Contains(m.status, "moved 1 message to trash") {
		t.Errorf("status = %q, want a move confirmation", m.status)
	}
	if !strings.Contains(m.View(), "no message selected") {
		t.Error("the empty reader does not say there is no message selected")
	}
}

// TestReaderDeleteLastOfThreadStaysInTheConversation proves deleting the message
// being read leaves the reader inside the same conversation, on the one above it.
func TestReaderDeleteLastOfThreadStaysInTheConversation(t *testing.T) {
	parent := threadMessage(1, "parent subject", "<p>", 1)
	reply := threadMessage(2, "reply subject", "<r>", 2, "<p>")
	reply2 := threadMessage(3, "reply two subject", "<r2>", 3, "<r>")

	m, _ := withDelete(t, []mail.Message{parent, reply, reply2})
	m.loadBody = uidBody()
	m.focus = focusReader
	m.messageCursor = 2

	m = confirmDeleteInReader(t, m)

	if m.focus != focusReader {
		t.Fatalf("focus = %v, want the reading pane to stay open", m.focus)
	}
	if got := m.rows[m.messageCursor].Message.UID; got != 2 {
		t.Errorf("cursor is on UID %d, want the previous message of the thread", got)
	}

	key := bodyKey{account: m.accountName(), folder: "INBOX", uid: 2}
	if _, ok := m.bodies[key]; !ok {
		t.Error("the previous message's body was not fetched")
	}
}

// TestDeletedBodyLeavesTheCache proves a deleted message's body does not linger
// in memory for the rest of the session.
func TestDeletedBodyLeavesTheCache(t *testing.T) {
	m, _ := withDelete(t, sampleMessages(2))
	m.loadBody = uidBody()
	m = openSelectedBody(t, m)

	key := m.selectedBodyKey()
	if _, ok := m.bodies[key]; !ok {
		t.Fatal("setup: the open message has no cached body")
	}

	m = confirmDeleteInReader(t, m)

	if _, ok := m.bodies[key]; ok {
		t.Error("the deleted message's body stayed cached")
	}
}

// TestDeletedMessageIsNotMarkedSeen proves a body fetch that was still in flight
// when the message was deleted cannot mark it read afterwards: the queued
// mark-as-read is dropped with the message.
func TestDeletedMessageIsNotMarkedSeen(t *testing.T) {
	m, _ := withDelete(t, sampleMessages(2))
	m.loadBody = uidBody()

	// Open the message but hold the fetch back, as a slow server would.
	next, fetch := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)
	if fetch == nil {
		t.Fatal("setup: enter returned no fetch command")
	}

	key := m.selectedBodyKey()
	if !m.markSeenOnLoad[key] {
		t.Fatal("setup: opening the message did not queue a mark-as-read")
	}

	m = confirmDeleteInReader(t, m)

	if m.markSeenOnLoad[key] {
		t.Error("a deleted message is still queued to be marked read")
	}

	// The stale fetch lands after the delete.
	next, cmd := m.Update(fetch())
	m = next.(Model)

	if cmd != nil {
		t.Error("the stale body fetch issued a command for a deleted message")
	}
}

// TestReaderFlagTogglesLikeTheList proves f is honoured in the reading pane: the
// footer already advertises it there.
func TestReaderFlagTogglesLikeTheList(t *testing.T) {
	m, store := withTriage(t, sampleMessages(2))
	m.loadBody = uidBody()
	m = openSelectedBody(t, m)

	next, cmd := m.Update(keyMsg("f"))
	m = next.(Model)
	if cmd == nil {
		t.Fatal("f in the reading pane returned no command")
	}
	m = deliver(t, m, cmd)

	if store.calls != 1 {
		t.Fatalf("store calls = %d, want 1", store.calls)
	}
	if len(store.uids) != 1 || store.uids[0] != 2 {
		t.Errorf("uids = %v, want the message being read", store.uids)
	}
	if store.flags[0] != imap.FlagFlagged || !store.add {
		t.Errorf("sent add=%v flags=%v, want +\\Flagged", store.add, store.flags)
	}
	if !m.messages[0].Flagged() {
		t.Error("the row was not flagged after the server confirmed")
	}
}

// TestReaderReadTogglesLikeTheList proves m is honoured in the reading pane for
// the same reason.
func TestReaderReadTogglesLikeTheList(t *testing.T) {
	m, store := withTriage(t, sampleMessages(2))
	m.loadBody = uidBody()
	m = openSelectedBody(t, m)

	next, cmd := m.Update(keyMsg("m"))
	m = next.(Model)
	if cmd == nil {
		t.Fatal("m in the reading pane returned no command")
	}
	m = deliver(t, m, cmd)

	if store.calls != 1 {
		t.Fatalf("store calls = %d, want 1", store.calls)
	}
	if len(store.uids) != 1 || store.uids[0] != 2 {
		t.Errorf("uids = %v, want the message being read", store.uids)
	}
	if !store.add || store.flags[0] != imap.FlagSeen {
		t.Errorf("sent add=%v flags=%v, want +\\Seen", store.add, store.flags)
	}
}
