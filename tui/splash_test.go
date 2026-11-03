package tui

import (
	"errors"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/renderorange/chute/config"
	"github.com/renderorange/chute/mail"
)

// splashConfig is a config whose account names a host, which is what the splash
// prints while it waits.
func splashConfig() *config.Config {
	cfg := testConfig()
	cfg.Accounts[0].IMAP.Host = "imap.fastmail.com"

	return cfg
}

// splashModel is a fresh model that has received a window size and nothing else:
// exactly the state the app is in while the first folder listing is in flight.
func splashModel(t *testing.T, width, height int) Model {
	t.Helper()

	m := New(splashConfig(), "v9.9.9")
	m.clock = func(time.Duration, func(time.Time) tea.Msg) tea.Cmd { return nil }

	next, _ := m.Update(tea.WindowSizeMsg{Width: width, Height: height})

	return next.(Model)
}

// TestSplashShowsNameVersionAndTheConnection pins the block: what the app is, which
// account it is opening, where it is dialling, and how to leave the screen.
func TestSplashShowsNameVersionAndTheConnection(t *testing.T) {
	view := splashModel(t, 120, 40).View()

	for _, want := range []string{
		"chute",
		"v9.9.9",
		"[fastmail]",
		"imap.fastmail.com",
		"connecting",
		"press any key to skip",
	} {
		if !strings.Contains(view, want) {
			t.Errorf("splash missing %q:\n%s", want, view)
		}
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
	m := sizedWith(splashConfig(), 120, 40)
	m.loadFolders = fakeFolders([]mail.Folder{{Name: "INBOX", Delimiter: '/'}}, nil)

	next, _ := m.Update(m.Init()())
	m = next.(Model)

	view := m.View()
	if strings.Contains(view, "press any key to skip") {
		t.Errorf("the splash survived a folder load:\n%s", view)
	}
	if !strings.Contains(view, "folders") || !strings.Contains(view, "INBOX") {
		t.Errorf("the main view did not replace the splash:\n%s", view)
	}
}

// TestSplashEndsOnAFailedFolderLoad keeps a refusal from leaving the user on a
// connecting screen: the main frame says what went wrong.
func TestSplashEndsOnAFailedFolderLoad(t *testing.T) {
	m := sizedWith(splashConfig(), 120, 40)
	m.loadFolders = fakeFolders(nil, errors.New("connection refused"))

	next, _ := m.Update(m.Init()())
	m = next.(Model)

	view := m.View()
	if strings.Contains(view, "press any key to skip") {
		t.Errorf("the splash survived a failed folder load:\n%s", view)
	}
	if !strings.Contains(view, "disconnected") || !strings.Contains(view, "connection refused") {
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
	if view := m.View(); !strings.Contains(view, "folders") || strings.Contains(view, "press any key to skip") {
		t.Errorf("the main view did not replace the splash:\n%s", view)
	}
}
