package tui

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/emersion/go-imap/v2"
	"github.com/renderorange/chute/config"
	"github.com/renderorange/chute/mail"
)

// fakeStore records flag changes so tests can prove one command carries the
// whole selection.
type fakeStore struct {
	calls  int
	folder string
	uids   []imap.UID
	add    bool
	flags  []imap.Flag
	err    error
}

// load stands in for the session-bound storeFlags loader.
func (f *fakeStore) load(_ config.Account, folder string, uids []imap.UID, add bool, flags ...imap.Flag) error {
	f.calls++
	f.folder = folder
	f.uids = append([]imap.UID(nil), uids...)
	f.add = add
	f.flags = append([]imap.Flag(nil), flags...)

	return f.err
}

// withTriage returns an opened INBOX wired to a recording flag loader.
func withTriage(t *testing.T, messages []mail.Message) (Model, *fakeStore) {
	t.Helper()

	store := &fakeStore{}
	m := openInbox(t, withInbox(t, messages, nil))
	m.storeFlags = store.load

	return m, store
}

// deliver runs a command and hands its message back to the model.
func deliver(t *testing.T, m Model, cmd tea.Cmd) Model {
	t.Helper()

	if cmd == nil {
		t.Fatal("command is nil")
	}

	next, _ := m.Update(cmd())

	return next.(Model)
}

// TestDefaultBindingsAreUnique proves no key is bound to two actions, which is
// what lets the keymap resolve a key in any order.
func TestDefaultBindingsAreUnique(t *testing.T) {
	owners := make(map[string]action)

	for act, keys := range defaultBindings() {
		for _, key := range keys {
			if other, taken := owners[key]; taken {
				t.Errorf("key %q is bound to %v and %v", key, other, act)
			}
			owners[key] = act
		}
	}
}

// TestRebindingChangesDispatch is the M6-readiness gate: the handler follows the
// binding, not the literal key.
func TestRebindingChangesDispatch(t *testing.T) {
	m, store := withTriage(t, sampleMessages(2))

	keys := defaultBindings()
	keys[actionToggleRead] = []string{"M"}
	m.keys = keys

	next, cmd := m.Update(keyMsg("M"))
	m = next.(Model)
	if cmd == nil {
		t.Fatal("the rebound key did not dispatch the action")
	}
	m = deliver(t, m, cmd)

	if store.calls != 1 {
		t.Errorf("store calls = %d, want 1 for the rebound key", store.calls)
	}

	next, cmd = m.Update(keyMsg("m"))
	m = next.(Model)
	if cmd != nil {
		t.Fatal("the previously bound key still dispatched the action")
	}
	if store.calls != 1 {
		t.Errorf("store calls = %d, want the old key to do nothing", store.calls)
	}
}

// TestToggleReadMarksUnreadMessage proves one keypress sends one add-flag
// command for the cursor row and updates the row afterwards.
func TestToggleReadMarksUnreadMessage(t *testing.T) {
	m, store := withTriage(t, sampleMessages(2))

	next, cmd := m.Update(keyMsg("m"))
	m = next.(Model)
	m = deliver(t, m, cmd)

	if store.calls != 1 {
		t.Fatalf("store calls = %d, want 1", store.calls)
	}
	if store.folder != "INBOX" {
		t.Errorf("folder = %q, want INBOX", store.folder)
	}
	if !store.add || len(store.flags) != 1 || store.flags[0] != imap.FlagSeen {
		t.Errorf("sent add=%v flags=%v, want +\\Seen", store.add, store.flags)
	}
	if len(store.uids) != 1 || store.uids[0] != 2 {
		t.Errorf("uids = %v, want the cursor row only", store.uids)
	}
	if !m.messages[0].Seen() {
		t.Error("the cursor row was not marked read after the server confirmed")
	}
	if m.messages[1].Seen() {
		t.Error("an unselected row was marked read")
	}
}

// TestToggleReadClearsWhenAllSeen proves the key is a toggle, not mark-read.
func TestToggleReadClearsWhenAllSeen(t *testing.T) {
	messages := sampleMessages(1)
	messages[0].Flags = []imap.Flag{imap.FlagSeen}
	m, store := withTriage(t, messages)

	next, cmd := m.Update(keyMsg("m"))
	m = next.(Model)
	m = deliver(t, m, cmd)

	if store.add {
		t.Error("add = true, want -\\Seen for an all-read selection")
	}
	if m.messages[0].Seen() {
		t.Error("the row stayed read")
	}
}

// TestToggleFlagFlipsFlagged proves the same toggle shape for \Flagged.
func TestToggleFlagFlipsFlagged(t *testing.T) {
	m, store := withTriage(t, sampleMessages(1))

	next, cmd := m.Update(keyMsg("f"))
	m = next.(Model)
	m = deliver(t, m, cmd)

	if !store.add || len(store.flags) != 1 || store.flags[0] != imap.FlagFlagged {
		t.Fatalf("sent add=%v flags=%v, want +\\Flagged", store.add, store.flags)
	}
	if !m.messages[0].Flagged() {
		t.Error("the row is not flagged after the server confirmed")
	}

	next, cmd = m.Update(keyMsg("f"))
	m = next.(Model)
	m = deliver(t, m, cmd)

	if store.add {
		t.Error("second press sent add = true, want -\\Flagged")
	}
	if m.messages[0].Flagged() {
		t.Error("the row stayed flagged")
	}
}

// TestVisualRangeSelectsRows proves v anchors a range that follows the cursor.
func TestVisualRangeSelectsRows(t *testing.T) {
	m, _ := withTriage(t, sampleMessages(4))

	next, _ := m.Update(keyMsg("v"))
	m = next.(Model)
	if !m.visualActive() {
		t.Fatal("v did not start a range")
	}

	for i := 0; i < 2; i++ {
		next, _ = m.Update(keyMsg("j"))
		m = next.(Model)
	}

	uids := m.actionUIDs()
	if len(uids) != 3 {
		t.Fatalf("selection = %v, want three rows", uids)
	}

	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = next.(Model)
	if m.visualActive() {
		t.Error("esc did not clear the range")
	}
	if got := len(m.actionUIDs()); got != 1 {
		t.Errorf("selection after esc = %d rows, want 1", got)
	}
}

// TestVisualRangeSendsOneCommand proves a range of twenty is one UID set, which
// is the milestone's bulk gate.
func TestVisualRangeSendsOneCommand(t *testing.T) {
	m, store := withTriage(t, sampleMessages(25))

	next, _ := m.Update(keyMsg("v"))
	m = next.(Model)
	for i := 0; i < 19; i++ {
		next, _ = m.Update(keyMsg("j"))
		m = next.(Model)
	}

	next, cmd := m.Update(keyMsg("m"))
	m = next.(Model)
	m = deliver(t, m, cmd)

	if store.calls != 1 {
		t.Errorf("store calls = %d, want one command for the whole range", store.calls)
	}
	if len(store.uids) != 20 {
		t.Errorf("uids = %d, want exactly 20", len(store.uids))
	}
	if m.visualActive() {
		t.Error("the range stayed active after the action ran")
	}
}

// TestRangeAboveAnchorSelectsRows proves a range works when the cursor moves up
// past the anchor.
func TestRangeAboveAnchorSelectsRows(t *testing.T) {
	m, _ := withTriage(t, sampleMessages(3))

	next, _ := m.Update(keyMsg("G"))
	m = next.(Model)
	next, _ = m.Update(keyMsg("v"))
	m = next.(Model)

	for i := 0; i < 2; i++ {
		next, _ = m.Update(keyMsg("k"))
		m = next.(Model)
	}

	if got := len(m.actionUIDs()); got != 3 {
		t.Errorf("selection = %d rows, want 3 when the cursor is above the anchor", got)
	}
}

// TestFlagsFailureKeepsRows proves a rejected command changes nothing and says
// so.
func TestFlagsFailureKeepsRows(t *testing.T) {
	m, store := withTriage(t, sampleMessages(1))
	store.err = errors.New("server said no")

	next, cmd := m.Update(keyMsg("m"))
	m = next.(Model)
	m = deliver(t, m, cmd)

	if m.messages[0].Seen() {
		t.Error("the row was marked read although the server rejected it")
	}
	if !strings.Contains(m.status, "server said no") {
		t.Errorf("status = %q, want the server error", m.status)
	}
}

// TestOpeningUnreadMessageMarksItSeen proves enter marks mail read through a
// separate command, after the body is fetched.
func TestOpeningUnreadMessageMarksItSeen(t *testing.T) {
	store := &fakeStore{}
	m := openInbox(t, withInbox(t, sampleMessages(1), nil))
	m.loadBody = fakeBody(sampleBody(), nil)
	m.storeFlags = store.load

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)
	if cmd == nil {
		t.Fatal("enter returned no body command")
	}

	next, markCmd := m.Update(cmd())
	m = next.(Model)
	if markCmd == nil {
		t.Fatal("opening an unread message sent no \\Seen command")
	}

	m = deliver(t, m, markCmd)

	if store.calls != 1 {
		t.Errorf("store calls = %d, want 1", store.calls)
	}
	if !store.add || len(store.flags) != 1 || store.flags[0] != imap.FlagSeen {
		t.Errorf("sent add=%v flags=%v, want +\\Seen", store.add, store.flags)
	}
	if !m.messages[0].Seen() {
		t.Error("the row is still unread after opening it")
	}
}

// TestOpeningReadMessageSendsNoStore proves a second open does not refetch or
// re-mark anything.
func TestOpeningReadMessageSendsNoStore(t *testing.T) {
	messages := sampleMessages(1)
	messages[0].Flags = []imap.Flag{imap.FlagSeen}

	store := &fakeStore{}
	m := openInbox(t, withInbox(t, messages, nil))
	m.loadBody = fakeBody(sampleBody(), nil)
	m.storeFlags = store.load

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)
	m = deliver(t, m, cmd)

	if store.calls != 0 {
		t.Errorf("store calls = %d, want 0 for an already-read message", store.calls)
	}
}

// TestVisualFooterShowsRangeKeys proves the footer reflects the active mode.
func TestVisualFooterShowsRangeKeys(t *testing.T) {
	m, _ := withTriage(t, sampleMessages(2))

	next, _ := m.Update(keyMsg("v"))
	m = next.(Model)

	if !strings.Contains(m.View(), "esc:cancel") {
		t.Error("the footer does not advertise esc:cancel while a range is active")
	}
}

// fakeDelete records delete calls so tests can prove nothing reaches the server
// before the confirmation is answered.
type fakeDelete struct {
	calls  int
	folder string
	uids   []imap.UID
	err    error
}

// load stands in for the session-bound deleteMessages loader.
func (f *fakeDelete) load(_ config.Account, folder string, uids []imap.UID) error {
	f.calls++
	f.folder = folder
	f.uids = append([]imap.UID(nil), uids...)

	return f.err
}

// withDelete returns an opened INBOX wired to a recording delete loader.
func withDelete(t *testing.T, messages []mail.Message) (Model, *fakeDelete) {
	t.Helper()

	remove := &fakeDelete{}
	m := openInbox(t, withInbox(t, messages, nil))
	m.deleteMessages = remove.load

	return m, remove
}

// TestDeleteAsksBeforeActing proves d never touches the server on its own.
func TestDeleteAsksBeforeActing(t *testing.T) {
	m, remove := withDelete(t, sampleMessages(3))

	next, cmd := m.Update(keyMsg("d"))
	m = next.(Model)

	if !m.confirmOpen {
		t.Fatal("d did not open the confirmation")
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

// TestDeleteCancelledByEscape proves esc abandons the action untouched.
func TestDeleteCancelledByEscape(t *testing.T) {
	m, remove := withDelete(t, sampleMessages(2))

	next, _ := m.Update(keyMsg("d"))
	m = next.(Model)

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = next.(Model)

	if m.confirmOpen {
		t.Error("esc did not close the confirmation")
	}
	if cmd != nil {
		t.Error("esc returned a command")
	}
	if remove.calls != 0 {
		t.Error("a cancelled delete still reached the server")
	}
	if len(m.messages) != 2 {
		t.Errorf("messages = %d, want both kept", len(m.messages))
	}
}

// TestDeleteConfirmedRemovesRow proves a confirmed delete removes the row and
// reports the move.
func TestDeleteConfirmedRemovesRow(t *testing.T) {
	m, remove := withDelete(t, sampleMessages(3))

	next, _ := m.Update(keyMsg("d"))
	m = next.(Model)

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)
	m = deliver(t, m, cmd)

	if remove.calls != 1 {
		t.Fatalf("delete calls = %d, want 1", remove.calls)
	}
	if remove.folder != "INBOX" {
		t.Errorf("folder = %q, want INBOX", remove.folder)
	}
	if len(remove.uids) != 1 {
		t.Errorf("uids = %v, want one", remove.uids)
	}
	if len(m.messages) != 2 {
		t.Errorf("messages = %d, want 2 left", len(m.messages))
	}
	if !strings.Contains(m.status, "moved 1 message to trash") {
		t.Errorf("status = %q, want a move confirmation", m.status)
	}
	if m.confirmOpen {
		t.Error("the confirmation stayed open after confirming")
	}
}

// TestDeleteRangeRemovesEveryRow proves a range delete removes exactly the
// selected rows in one command.
func TestDeleteRangeRemovesEveryRow(t *testing.T) {
	m, remove := withDelete(t, sampleMessages(5))

	next, _ := m.Update(keyMsg("v"))
	m = next.(Model)
	for i := 0; i < 2; i++ {
		next, _ = m.Update(keyMsg("j"))
		m = next.(Model)
	}

	next, _ = m.Update(keyMsg("d"))
	m = next.(Model)
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)
	m = deliver(t, m, cmd)

	if remove.calls != 1 {
		t.Errorf("delete calls = %d, want one command for the range", remove.calls)
	}
	if len(remove.uids) != 3 {
		t.Errorf("uids = %v, want three", remove.uids)
	}
	if len(m.messages) != 2 {
		t.Errorf("messages = %d, want 2 left", len(m.messages))
	}
	if m.visualActive() {
		t.Error("the range stayed active after the delete")
	}
}

// TestDeleteFailureKeepsRows proves a rejected delete leaves the list whole and
// reports the error.
func TestDeleteFailureKeepsRows(t *testing.T) {
	m, remove := withDelete(t, sampleMessages(2))
	remove.err = errors.New("server said no")

	next, _ := m.Update(keyMsg("d"))
	m = next.(Model)
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)
	m = deliver(t, m, cmd)

	if len(m.messages) != 2 {
		t.Errorf("messages = %d, want the list unchanged", len(m.messages))
	}
	if !strings.Contains(m.status, "server said no") {
		t.Errorf("status = %q, want the server error", m.status)
	}
}

// TestDeleteOnEmptyFolderReports proves the key reports instead of opening a
// confirmation for nothing.
func TestDeleteOnEmptyFolderReports(t *testing.T) {
	m, remove := withDelete(t, nil)

	next, cmd := m.Update(keyMsg("d"))
	m = next.(Model)

	if m.confirmOpen {
		t.Error("a confirmation opened with no messages")
	}
	if cmd != nil || remove.calls != 0 {
		t.Error("a delete was attempted with no messages")
	}
	if m.status == "" {
		t.Error("no status was set for an empty selection")
	}
}

// TestTriageKeysIgnoreOtherPanels proves a triage key does nothing while the
// folder list owns the selection: pressing m there must not mark mail read.
func TestTriageKeysIgnoreOtherPanels(t *testing.T) {
	m, store := withTriage(t, sampleMessages(2))

	next, _ := m.Update(keyMsg("tab"))
	m = next.(Model)
	for m.focus != focusFolders {
		next, _ = m.Update(keyMsg("tab"))
		m = next.(Model)
	}

	next, cmd := m.Update(keyMsg("m"))
	m = next.(Model)

	if cmd != nil || store.calls != 0 {
		t.Error("m changed flags while the folder list had focus")
	}

	next, cmd = m.Update(keyMsg("d"))
	m = next.(Model)
	if m.confirmOpen || cmd != nil {
		t.Error("d opened a confirmation while the folder list had focus")
	}
}

// TestQuitClosesModalInsteadOfExiting proves q keeps its old modal meaning: it
// closes the picker rather than killing the program.
func TestQuitClosesModalInsteadOfExiting(t *testing.T) {
	m, remove := withDelete(t, sampleMessages(2))

	next, _ := m.Update(keyMsg("d"))
	m = next.(Model)

	next, cmd := m.Update(keyMsg("q"))
	m = next.(Model)

	if cmd != nil {
		t.Error("q in a modal returned a command")
	}
	if m.confirmOpen {
		t.Error("q did not close the confirmation")
	}
	if remove.calls != 0 {
		t.Error("q in the confirmation still deleted")
	}
}

// TestForceQuitLeavesFromModal proves ctrl+c still exits from a modal.
func TestForceQuitLeavesFromModal(t *testing.T) {
	m, _ := withDelete(t, sampleMessages(1))

	next, _ := m.Update(keyMsg("d"))
	m = next.(Model)

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	_ = next

	if cmd == nil {
		t.Fatal("ctrl+c in a modal returned no command")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Error("ctrl+c in a modal did not quit the program")
	}
}

// TestOpeningMessageClearsVisualRange proves enter ends the selection, so the
// first esc afterwards leaves the reader instead of clearing an invisible range.
func TestOpeningMessageClearsVisualRange(t *testing.T) {
	m, _ := withTriage(t, sampleMessages(3))
	m.loadBody = fakeBody(sampleBody(), nil)

	next, _ := m.Update(keyMsg("v"))
	m = next.(Model)
	next, _ = m.Update(keyMsg("j"))
	m = next.(Model)

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)
	m = deliver(t, m, cmd)

	if m.visualActive() {
		t.Error("the range survived opening a message")
	}

	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = next.(Model)
	if m.focus != focusMessages {
		t.Errorf("focus = %v, want messages after one esc from the reader", m.focus)
	}
}

// TestBodyFailureClearsPendingSeen proves a failed load leaves no stale
// mark-on-open intent behind.
func TestBodyFailureClearsPendingSeen(t *testing.T) {
	m := openInbox(t, withInbox(t, sampleMessages(1), nil))
	m.loadBody = fakeBody(nil, errors.New("fetch failed"))
	m.storeFlags = (&fakeStore{}).load

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)
	m = deliver(t, m, cmd)

	if m.markSeenOnLoad != (bodyKey{}) {
		t.Error("a failed body load left a pending mark-on-open")
	}
	if !strings.Contains(m.status, "fetch failed") {
		t.Errorf("status = %q, want the load error", m.status)
	}
}

// TestImplicitMarkSeenFailureIsQuiet proves a read-only folder does not report an
// error every time a message is opened.
func TestImplicitMarkSeenFailureIsQuiet(t *testing.T) {
	store := &fakeStore{err: errors.New("mailbox is read-only")}
	m := openInbox(t, withInbox(t, sampleMessages(1), nil))
	m.loadBody = fakeBody(sampleBody(), nil)
	m.storeFlags = store.load

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)
	next, markCmd := m.Update(cmd())
	m = next.(Model)
	m = deliver(t, m, markCmd)

	if store.calls != 1 {
		t.Fatalf("store calls = %d, want 1", store.calls)
	}
	if strings.Contains(m.status, "read-only") {
		t.Errorf("status = %q, want an implicit failure kept quiet", m.status)
	}
}

// TestExplicitFlagFailureIsNotQuiet proves a flag change the user asked for does
// report its error.
func TestExplicitFlagFailureIsNotQuiet(t *testing.T) {
	m, store := withTriage(t, sampleMessages(1))
	store.err = errors.New("mailbox is read-only")

	next, cmd := m.Update(keyMsg("m"))
	m = next.(Model)
	m = deliver(t, m, cmd)

	if !strings.Contains(m.status, "read-only") {
		t.Errorf("status = %q, want the error from an explicit flag change", m.status)
	}
}

// TestTriageKeysAreInertWithoutMessages proves a keypress on an empty folder
// reports rather than calling the server.
func TestTriageKeysAreInertWithoutMessages(t *testing.T) {
	m, store := withTriage(t, nil)

	next, cmd := m.Update(keyMsg("m"))
	m = next.(Model)

	if cmd != nil || store.calls != 0 {
		t.Error("a flag change was attempted with no messages")
	}
	if m.status == "" {
		t.Error("no status was set for an empty selection")
	}
}
