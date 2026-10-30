package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
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

// sized returns a model that has received a window size.
func sized(width, height int) Model {
	m := New(testConfig(), "test")
	next, _ := m.Update(tea.WindowSizeMsg{Width: width, Height: height})

	return next.(Model)
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

func TestAccountKeySetsStatus(t *testing.T) {
	next, _ := sized(140, 40).Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	if got := next.(Model).status; got == "" {
		t.Error("a did not set a status message")
	}

	next, _ = next.(Model).Update(tea.KeyMsg{Type: tea.KeyEsc})
	if got := next.(Model).status; got != "" {
		t.Errorf("esc left status = %q, want empty", got)
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

	m := New(cfg, "test")
	next, _ := m.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
	m = next.(Model)
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

	m := New(cfg, "test")
	next, _ := m.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
	m = next.(Model)
	m.loadFolders = fakeFolders([]mail.Folder{{Name: "INBOX", Delimiter: '/'}}, nil)

	next, _ = m.Update(m.Init()())
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

	m := New(cfg, "test")
	next, _ := m.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
	m = next.(Model)
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
	next, cmd = m.Update(foldersMsg{account: "fastmail", folders: []mail.Folder{{Name: "INBOX", Delimiter: '/'}}})
	m = next.(Model)

	if cmd != nil {
		t.Error("a later folder load reopened the start folder")
	}
}
