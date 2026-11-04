package tui

import (
	"errors"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/renderorange/chute/mail"
)

// splashModel is a fresh model that has received a window size and nothing else:
// exactly the state the app is in while the first folder listing is in flight.
func splashModel(t *testing.T, width, height int) Model {
	t.Helper()

	m := New(testConfig(), "v9.9.9")
	m.clock = func(time.Duration, func(time.Time) tea.Msg) tea.Cmd { return nil }

	next, _ := m.Update(tea.WindowSizeMsg{Width: width, Height: height})

	return next.(Model)
}

// contentLines returns the view's non-blank lines with the frame padding trimmed
// away, so a content assertion does not count the centring.
func contentLines(view string) []string {
	lines := make([]string, 0, 4)
	for _, line := range strings.Split(view, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			lines = append(lines, line)
		}
	}

	return lines
}

// TestSplashShowsOnlyNameAndBuild is the issue #93 contract: the app name and the
// build version, and no account, host, connection state or skip instruction.
func TestSplashShowsOnlyNameAndBuild(t *testing.T) {
	view := splashModel(t, 120, 40).View()

	got := contentLines(view)
	want := []string{"chute", "v9.9.9"}

	if len(got) != len(want) {
		t.Fatalf("splash has %d content lines, want %d:\n%s", len(got), len(want), view)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("splash line %d = %q, want %q", i, got[i], want[i])
		}
	}
}

// TestSplashShowsDevWhenTheBuildHasNoVersion keeps the second line meaningful for an
// unversioned build instead of rendering an empty row.
func TestSplashShowsDevWhenTheBuildHasNoVersion(t *testing.T) {
	m := New(testConfig(), "")
	next, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})

	got := contentLines(next.(Model).View())
	if len(got) != 2 || got[1] != "dev" {
		t.Errorf("splash = %q, want [chute dev]", got)
	}
}

// TestSplashIsAWholeFrame holds the splash to the same geometry contract as every
// other screen: terminal width, terminal height, at the minimum size and above.
func TestSplashIsAWholeFrame(t *testing.T) {
	for _, size := range [][2]int{{60, 20}, {105, 59}, {140, 40}} {
		assertExactFrame(t, splashModel(t, size[0], size[1]), "splash")
	}
}

// TestSplashEndsWhenFoldersArrive proves the splash is a waiting screen, not a
// screen with a timer: the first successful listing replaces it.
func TestSplashEndsWhenFoldersArrive(t *testing.T) {
	m := splashModel(t, 120, 40)
	m.loadFolders = fakeFolders([]mail.Folder{{Name: "INBOX", Delimiter: '/'}}, nil)

	next, _ := m.Update(m.Init()())
	m = next.(Model)

	if m.splash {
		t.Error("the splash survived a folder load")
	}
	if view := m.View(); !strings.Contains(view, "folders") || !strings.Contains(view, "INBOX") {
		t.Errorf("the main view did not replace the splash:\n%s", view)
	}
}

// TestSplashEndsOnAFailedFolderLoad keeps a refusal from leaving the user on a
// connecting screen: the main frame says what went wrong.
func TestSplashEndsOnAFailedFolderLoad(t *testing.T) {
	m := splashModel(t, 120, 40)
	m.loadFolders = fakeFolders(nil, errors.New("connection refused"))

	next, _ := m.Update(m.Init()())
	m = next.(Model)

	if m.splash {
		t.Error("the splash survived a failed folder load")
	}
	if view := m.View(); !strings.Contains(view, "disconnected") || !strings.Contains(view, "connection refused") {
		t.Errorf("the failure is not on screen:\n%s", view)
	}
}

// TestAnyKeySkipsTheSplash is the imap-notes behaviour: the first keypress replaces
// the splash and is consumed, so q skips rather than quits.
func TestAnyKeySkipsTheSplash(t *testing.T) {
	next, cmd := splashModel(t, 120, 40).Update(keyMsg("q"))
	m := next.(Model)

	if cmd != nil {
		t.Error("the key that skipped the splash was also dispatched")
	}
	if m.splash {
		t.Error("the key did not clear the splash")
	}
	if view := m.View(); !strings.Contains(view, "folders") {
		t.Errorf("the main view did not replace the splash:\n%s", view)
	}
}
