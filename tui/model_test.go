package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/renderorange/chute/config"
)

// testConfig builds a two-account config without touching disk.
func testConfig() *config.Config {
	return &config.Config{
		General: config.General{PollInterval: 60},
		Accounts: []config.Account{
			{Name: "fastmail", Default: true},
			{Name: "work"},
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
	view := sized(90, 30).View()
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
	m := sized(90, 30)

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

	next, _ := m.Update(tea.WindowSizeMsg{Width: 90, Height: 30})
	if got := next.(Model).focus; got != focusMessages {
		t.Errorf("focus at 90 columns = %v, want messages", got)
	}

	next, _ = next.(Model).Update(tea.WindowSizeMsg{Width: 50, Height: 20})
	if got := next.(Model).focus; got != focusMessages {
		t.Errorf("focus at 50 columns = %v, want messages", got)
	}
}

func TestTabCyclesInSingleMode(t *testing.T) {
	m := sized(50, 20)

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyTab})
	if got := next.(Model).focus; got != focusReader {
		t.Errorf("focus after one tab = %v, want reader", got)
	}

	next, _ = next.(Model).Update(tea.KeyMsg{Type: tea.KeyTab})
	if got := next.(Model).focus; got != focusMessages {
		t.Errorf("focus after two tabs = %v, want messages", got)
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
		{"truncated", 5, "trun…"},
		{"abc", 1, "a"},
		{"abc", 0, ""},
	}

	for _, tc := range tests {
		if got := Truncate(tc.in, tc.width); got != tc.want {
			t.Errorf("Truncate(%q, %d) = %q, want %q", tc.in, tc.width, got, tc.want)
		}
	}
}
