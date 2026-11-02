package tui

import (
	"errors"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/emersion/go-imap/v2"
	"github.com/renderorange/chute/config"
	"github.com/renderorange/chute/mail"
)

// startFolder returns a pointer for an account's start_folder setting.
func startFolder(name string) *string {
	return &name
}

// testConfig builds a two-account config without touching disk.
func testConfig() *config.Config {
	return &config.Config{
		General: config.General{PollInterval: 60},
		Accounts: []config.Account{
			{Name: "fastmail", Default: true, StartFolder: startFolder("")},
			{Name: "work", StartFolder: startFolder("")},
		},
	}
}

// sizedWith returns a model for cfg that has received a window size and whose
// clock never fires, so no test waits on a real timer. A test that wants to
// observe polling sets its own clock.
func sizedWith(cfg *config.Config, width, height int) Model {
	m := New(cfg, "test")
	m.clock = func(time.Duration, func(time.Time) tea.Msg) tea.Cmd { return nil }
	next, _ := m.Update(tea.WindowSizeMsg{Width: width, Height: height})

	return next.(Model)
}

// sized returns a model that has received a window size.
func sized(width, height int) Model {
	return sizedWith(testConfig(), width, height)
}

// firstContentLine returns the first non-blank line of the view, which is where the
// panel titles sit.
func firstContentLine(view string) string {
	for _, line := range strings.Split(view, "\n") {
		if strings.TrimSpace(line) != "" {
			return line
		}
	}

	return ""
}

func TestViewRendersThreePanels(t *testing.T) {
	view := sized(140, 40).View()
	titleRow := firstContentLine(view)

	for _, want := range []string{"folders", "messages", "reader"} {
		if !strings.Contains(titleRow, want) {
			t.Errorf("title row %q missing %q", titleRow, want)
		}
	}
	for _, want := range []string{"q:quit", "[fastmail]"} {
		if !strings.Contains(view, want) {
			t.Errorf("view missing %q", want)
		}
	}
}

func TestViewDropsFoldersInTwoColumnMode(t *testing.T) {
	view := sized(110, 30).View()
	titleRow := firstContentLine(view)

	if strings.Contains(titleRow, "folders") {
		t.Errorf("two-column title row %q should not name folders", titleRow)
	}
	for _, want := range []string{"messages", "reader"} {
		if !strings.Contains(titleRow, want) {
			t.Errorf("title row %q missing %q", titleRow, want)
		}
	}
	if !strings.Contains(view, `\:folders`) {
		t.Error("two-column footer should advertise the folders key")
	}
}

func TestViewBeforeWindowSize(t *testing.T) {
	view := New(testConfig(), "test").View()

	if !strings.Contains(view, "chute test") {
		t.Errorf("placeholder view = %q, want it to name the version", view)
	}
}

func TestQuitKey(t *testing.T) {
	_, cmd := sized(140, 40).Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})

	if cmd == nil {
		t.Fatal("q returned no command")
	}
	if msg := cmd(); msg != tea.Quit() {
		t.Errorf("q command produced %v, want tea.Quit", msg)
	}
}

func TestTabCyclesFocusInThreeColumnMode(t *testing.T) {
	m := sized(140, 40)

	if m.focus != focusFolders {
		t.Fatalf("initial focus = %v, want folders", m.focus)
	}

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyTab})
	if got := next.(Model).focus; got != focusMessages {
		t.Errorf("focus after one tab = %v, want messages", got)
	}

	next, _ = next.(Model).Update(tea.KeyMsg{Type: tea.KeyTab})
	if got := next.(Model).focus; got != focusReader {
		t.Errorf("focus after two tabs = %v, want reader", got)
	}

	next, _ = next.(Model).Update(tea.KeyMsg{Type: tea.KeyTab})
	if got := next.(Model).focus; got != focusFolders {
		t.Errorf("focus after three tabs = %v, want folders", got)
	}
}

func TestTabSkipsFoldersInTwoColumnMode(t *testing.T) {
	m := sized(110, 30)

	if m.focus != focusMessages {
		t.Fatalf("initial focus = %v, want messages after clamping", m.focus)
	}

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyTab})
	if got := next.(Model).focus; got != focusReader {
		t.Errorf("focus after one tab = %v, want reader", got)
	}

	next, _ = next.(Model).Update(tea.KeyMsg{Type: tea.KeyTab})
	if got := next.(Model).focus; got != focusMessages {
		t.Errorf("focus after two tabs = %v, want messages", got)
	}
}

func TestFocusClampsWhenNarrowing(t *testing.T) {
	m := sized(140, 40)
	if m.focus != focusFolders {
		t.Fatalf("initial focus = %v, want folders", m.focus)
	}

	next, _ := m.Update(tea.WindowSizeMsg{Width: 110, Height: 30})
	if got := next.(Model).focus; got != focusMessages {
		t.Errorf("focus at 110 columns = %v, want messages", got)
	}

	next, _ = next.(Model).Update(tea.WindowSizeMsg{Width: 50, Height: 20})
	if got := next.(Model).focus; got != focusMessages {
		t.Errorf("focus at 50 columns = %v, want messages", got)
	}
}

func TestTabCyclesInSingleMode(t *testing.T) {
	m := sized(50, 20)

	if m.focus != focusFolders {
		t.Fatalf("initial focus = %v, want folders", m.focus)
	}

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyTab})
	if got := next.(Model).focus; got != focusMessages {
		t.Errorf("focus after one tab = %v, want messages", got)
	}

	next, _ = next.(Model).Update(tea.KeyMsg{Type: tea.KeyTab})
	if got := next.(Model).focus; got != focusReader {
		t.Errorf("focus after two tabs = %v, want reader", got)
	}

	next, _ = next.(Model).Update(tea.KeyMsg{Type: tea.KeyTab})
	if got := next.(Model).focus; got != focusFolders {
		t.Errorf("focus after three tabs = %v, want folders", got)
	}
}

func TestSingleModeShowsFocusedPanel(t *testing.T) {
	m := loaded(t, 50, 20, sampleFolders(3), nil)

	if m.focus != focusFolders {
		t.Fatalf("focus = %v, want folders", m.focus)
	}
	if title := firstContentLine(m.View()); !strings.Contains(title, "folders") {
		t.Errorf("single-mode title = %q, want folders", title)
	}
	if view := m.View(); !strings.Contains(view, "INBOX") {
		t.Error("single-mode view missing INBOX")
	}

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyTab})
	if title := firstContentLine(next.(Model).View()); !strings.Contains(title, "messages") {
		t.Errorf("title after tab = %q, want messages", title)
	}
}

func TestTruncate(t *testing.T) {
	tests := []struct {
		in    string
		width int
		want  string
	}{
		{"short", 10, "short"},
		{"exactly10!", 10, "exactly10!"},
		{"truncated", 5, "tr..."},
		{"abcdef", 3, "abc"},
		{"abc", 2, "ab"},
		{"abc", 1, "a"},
		{"abc", 0, ""},
	}

	for _, tc := range tests {
		if got := Truncate(tc.in, tc.width); got != tc.want {
			t.Errorf("Truncate(%q, %d) = %q, want %q", tc.in, tc.width, got, tc.want)
		}
	}
}

func TestNewWiresPersistentLoaders(t *testing.T) {
	m := New(testConfig(), "test")

	if m.sessions == nil {
		t.Error("session manager is nil")
	}
	if m.loadFolders == nil || m.loadMessages == nil || m.loadBody == nil {
		t.Error("loaders are not wired")
	}
}

func TestCloseWithoutConnections(t *testing.T) {
	if err := New(testConfig(), "test").Close(); err != nil {
		t.Errorf("Close on a fresh model = %v, want nil", err)
	}
}

func TestFoldersLoadOpensStartFolder(t *testing.T) {
	cfg := testConfig()
	cfg.Accounts[0].StartFolder = nil

	m := sizedWith(cfg, 140, 40)
	m.loadFolders = fakeFolders([]mail.Folder{
		{Name: "INBOX", Delimiter: '/'},
		{Name: "Work", Delimiter: '/'},
	}, nil)
	m.loadMessages = fakeMessages(sampleMessages(2), nil)

	next, cmd := m.Update(m.Init()())
	m = next.(Model)

	if got, want := m.openFolder, "INBOX"; got != want {
		t.Fatalf("openFolder = %q, want %q", got, want)
	}
	if cmd == nil {
		t.Fatal("the folder load did not start the message fetch")
	}

	next, _ = m.Update(cmd())
	m = next.(Model)

	if got, want := len(m.messages), 2; got != want {
		t.Errorf("messages = %d, want %d", got, want)
	}
}

func TestStartFolderNotFound(t *testing.T) {
	cfg := testConfig()
	cfg.Accounts[0].StartFolder = startFolder("Nope")

	m := sizedWith(cfg, 140, 40)
	m.loadFolders = fakeFolders([]mail.Folder{{Name: "INBOX", Delimiter: '/'}}, nil)

	next, _ := m.Update(m.Init()())
	m = next.(Model)

	if m.openFolder != "" {
		t.Errorf("openFolder = %q, want empty", m.openFolder)
	}
	if !strings.Contains(m.status, "not found") {
		t.Errorf("status = %q, want a not-found message", m.status)
	}
}

func TestStartFolderOpensOnlyOnce(t *testing.T) {
	cfg := testConfig()
	cfg.Accounts[0].StartFolder = nil

	m := sizedWith(cfg, 140, 40)
	m.loadFolders = fakeFolders([]mail.Folder{{Name: "INBOX", Delimiter: '/'}}, nil)
	m.loadMessages = fakeMessages(sampleMessages(1), nil)

	next, cmd := m.Update(m.Init()())
	m = next.(Model)
	if cmd == nil {
		t.Fatal("the first folder load did not open the start folder")
	}
	next, _ = m.Update(cmd())
	m = next.(Model)

	// A later folder load, as after a reload, must not reopen the start folder.
	m.openFolder = ""
	m.messages = nil
	m.rows = nil
	next, cmd = m.Update(foldersMsg{account: "fastmail", folders: []mail.Folder{{Name: "INBOX", Delimiter: '/'}}})
	m = next.(Model)

	if cmd != nil {
		t.Error("a later folder load reopened the start folder")
	}
}

// readerThreadModel loads a three-message conversation with the cursor on its
// first row and the reader focused.
func readerThreadModel(t *testing.T) Model {
	t.Helper()

	parent := threadMessage(1, "parent subject", "<p>", 1)
	reply := threadMessage(2, "reply subject", "<r>", 2, "<p>")
	reply2 := threadMessage(3, "reply two subject", "<r2>", 3, "<r>")

	m := openInbox(t, withInbox(t, []mail.Message{parent, reply, reply2}, nil))
	m.focus = focusReader
	m.messageCursor = 0

	return m
}

func TestNextThreadMessage(t *testing.T) {
	m := readerThreadModel(t)

	for want := 1; want <= 2; want++ {
		next, _ := m.Update(keyMsg("n"))
		m = next.(Model)

		if got := m.messageCursor; got != want {
			t.Fatalf("after n, cursor = %d, want %d", got, want)
		}
		if m.focus != focusReader {
			t.Fatalf("n left the reader for focus %v", m.focus)
		}
	}

	// The last message of the thread has no next message.
	next, cmd := m.Update(keyMsg("n"))
	m = next.(Model)

	if got := m.messageCursor; got != 2 {
		t.Errorf("n past the last message moved the cursor to %d, want 2", got)
	}
	if cmd != nil {
		t.Error("n past the last message returned a command")
	}
}

func TestPrevThreadMessage(t *testing.T) {
	m := readerThreadModel(t)
	m.messageCursor = 2

	for want := 1; want >= 0; want-- {
		next, _ := m.Update(keyMsg("p"))
		m = next.(Model)

		if got := m.messageCursor; got != want {
			t.Fatalf("after p, cursor = %d, want %d", got, want)
		}
	}

	next, _ := m.Update(keyMsg("p"))
	if got := next.(Model).messageCursor; got != 0 {
		t.Errorf("p before the first message moved the cursor to %d, want 0", got)
	}
}

// TestThreadNavigationStaysInsideThread proves n stops at the end of a
// conversation instead of walking into the next one in the list.
func TestThreadNavigationStaysInsideThread(t *testing.T) {
	aParent := threadMessage(1, "a parent", "<a1>", 4)
	aReply := threadMessage(2, "a reply", "<a2>", 5, "<a1>")
	bParent := threadMessage(3, "b parent", "<b1>", 1)

	m := openInbox(t, withInbox(t, []mail.Message{aParent, aReply, bParent}, nil))
	m.focus = focusReader
	m.messageCursor = 1

	next, _ := m.Update(keyMsg("n"))
	if got := next.(Model).messageCursor; got != 1 {
		t.Errorf("n walked from the last reply into the next conversation: cursor = %d, want 1", got)
	}

	next, _ = m.Update(keyMsg("p"))
	if got := next.(Model).messageCursor; got != 0 {
		t.Errorf("p from the last reply = %d, want the parent at 0", got)
	}
}

// TestThreadKeysInertOnMessageList proves n and p only act inside the reader.
func TestThreadKeysInertOnMessageList(t *testing.T) {
	m := readerThreadModel(t)
	m.focus = focusMessages

	next, _ := m.Update(keyMsg("n"))

	if got := next.(Model).messageCursor; got != 0 {
		t.Errorf("n moved the list cursor to %d while the list had focus", got)
	}
}

// TestReaderEscLandsOnExpandedMessage proves backing out of the reader leaves the
// list cursor on the message that was last read.
func TestReaderEscLandsOnExpandedMessage(t *testing.T) {
	m := readerThreadModel(t)

	next, _ := m.Update(keyMsg("n"))
	m = next.(Model)

	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = next.(Model)

	if m.focus != focusMessages {
		t.Errorf("focus after esc = %v, want messages", m.focus)
	}
	if got := m.messageCursor; got != 1 {
		t.Errorf("cursor after esc = %d, want the message just read at 1", got)
	}
}

// TestTwoPendingMarkSeenIntents proves two in-flight body loads each queue their
// own mark-as-read, so walking a thread cannot lose one. The intent used to be a
// single slot.
func TestTwoPendingMarkSeenIntents(t *testing.T) {
	m := openInbox(t, withInbox(t, sampleMessages(2), nil))

	var stored []imap.UID
	m.storeFlags = func(_ config.Account, _ string, uids []imap.UID, _ bool, _ ...imap.Flag) error {
		stored = append(stored, uids...)

		return nil
	}

	first := bodyKey{account: m.accountName(), folder: "INBOX", uid: 1}
	second := bodyKey{account: m.accountName(), folder: "INBOX", uid: 2}
	m.markSeenOnLoad = map[bodyKey]bool{first: true, second: true}

	for _, uid := range []imap.UID{1, 2} {
		next, cmd := m.Update(bodyMsg{
			account: m.accountName(),
			folder:  "INBOX",
			uid:     uid,
			body:    &mail.Body{Text: "text"},
		})
		m = next.(Model)

		if cmd == nil {
			t.Fatalf("body for UID %d returned no mark-seen command", uid)
		}
		cmd()
	}

	if len(stored) != 2 || stored[0] != 1 || stored[1] != 2 {
		t.Errorf("marked %v read, want [1 2]", stored)
	}
	if len(m.markSeenOnLoad) != 0 {
		t.Errorf("mark intents left over: %v", m.markSeenOnLoad)
	}
}

// TestFailedBodyClearsOnlyItsOwnIntent proves one message's failed body load does
// not clear another message's pending mark.
func TestFailedBodyClearsOnlyItsOwnIntent(t *testing.T) {
	m := openInbox(t, withInbox(t, sampleMessages(2), nil))

	first := bodyKey{account: m.accountName(), folder: "INBOX", uid: 1}
	second := bodyKey{account: m.accountName(), folder: "INBOX", uid: 2}
	m.markSeenOnLoad = map[bodyKey]bool{first: true, second: true}

	next, _ := m.Update(bodyMsg{
		account: m.accountName(),
		folder:  "INBOX",
		uid:     1,
		err:     errors.New("boom"),
	})
	m = next.(Model)

	if m.markSeenOnLoad[first] {
		t.Error("the failed message kept its mark intent")
	}
	if !m.markSeenOnLoad[second] {
		t.Error("a failed body load cleared another message's mark intent")
	}
}
