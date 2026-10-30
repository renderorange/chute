package tui

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/emersion/go-imap/v2"
	"github.com/renderorange/chute/config"
	"github.com/renderorange/chute/mail"
)

// fakeFolders returns a loader that always yields the given folders and error.
func fakeFolders(folders []mail.Folder, err error) loadFoldersFunc {
	return func(config.Account) ([]mail.Folder, error) {
		return folders, err
	}
}

// sampleFolders builds n folders, the first being INBOX.
func sampleFolders(n int) []mail.Folder {
	folders := make([]mail.Folder, 0, n)
	folders = append(folders, mail.Folder{Name: "INBOX", Delimiter: '/'})

	for i := 1; i < n; i++ {
		folders = append(folders, mail.Folder{
			Name:      fmt.Sprintf("folder%02d", i),
			Delimiter: '/',
		})
	}

	return folders
}

// loaded returns a model that has finished loading folders.
func loaded(t *testing.T, width, height int, folders []mail.Folder, err error) Model {
	t.Helper()

	m := sized(width, height)
	m.loadFolders = fakeFolders(folders, err)

	cmd := m.Init()
	if cmd == nil {
		t.Fatal("Init returned no command")
	}

	next, _ := m.Update(cmd())

	return next.(Model)
}

func TestInitLoadsFolders(t *testing.T) {
	folders := []mail.Folder{
		{Name: "Archive", Delimiter: '/', Attrs: []imap.MailboxAttr{imap.MailboxAttrArchive}},
		{Name: "INBOX", Delimiter: '/'},
	}

	m := loaded(t, 140, 40, folders, nil)

	if m.state != stateConnected {
		t.Errorf("state = %v, want connected", m.state)
	}
	if len(m.folders) != 2 {
		t.Fatalf("folders = %d, want 2", len(m.folders))
	}

	view := m.View()
	for _, want := range []string{"INBOX", "Archive", "2 folders"} {
		if !strings.Contains(view, want) {
			t.Errorf("view missing %q", want)
		}
	}
}

func TestInitReportsLoadError(t *testing.T) {
	m := loaded(t, 140, 40, nil, errors.New("login as blaine failed"))

	if m.state != stateFailed {
		t.Errorf("state = %v, want failed", m.state)
	}
	if !strings.Contains(m.status, "login") {
		t.Errorf("status = %q, want the loader error", m.status)
	}

	view := m.View()
	for _, want := range []string{"disconnected", "login as blaine failed"} {
		if !strings.Contains(view, want) {
			t.Errorf("view missing %q", want)
		}
	}
}

func TestStaleFoldersMessageIgnored(t *testing.T) {
	m := sized(140, 40)
	m.state = stateConnecting

	next, _ := m.Update(foldersMsg{account: "someone-else", folders: sampleFolders(3)})
	got := next.(Model)

	if got.state != stateConnecting {
		t.Errorf("state = %v, want still connecting", got.state)
	}
	if len(got.folders) != 0 {
		t.Errorf("folders = %d, want 0 for a stale account", len(got.folders))
	}
}

func TestFolderCursorClamps(t *testing.T) {
	m := loaded(t, 140, 40, sampleFolders(5), nil)

	up, _ := m.Update(keyMsg("k"))
	if got := up.(Model).folderCursor; got != 0 {
		t.Errorf("cursor after k at top = %d, want 0", got)
	}

	for i := 0; i < 20; i++ {
		next, _ := m.Update(keyMsg("j"))
		m = next.(Model)
	}
	if got, want := m.folderCursor, 4; got != want {
		t.Errorf("cursor after many j = %d, want %d", got, want)
	}

	end, _ := m.Update(keyMsg("g"))
	if got := end.(Model).folderCursor; got != 0 {
		t.Errorf("cursor after g = %d, want 0", got)
	}

	last, _ := m.Update(keyMsg("G"))
	if got := last.(Model).folderCursor; got != 4 {
		t.Errorf("cursor after G = %d, want 4", got)
	}
}

func TestFolderScrollFollowsCursor(t *testing.T) {
	// A short terminal means only a few folder lines fit.
	m := loaded(t, 140, 16, sampleFolders(40), nil)

	if m.folderScroll != 0 {
		t.Fatalf("initial scroll = %d, want 0", m.folderScroll)
	}

	for i := 0; i < 20; i++ {
		next, _ := m.Update(keyMsg("j"))
		m = next.(Model)
	}

	if m.folderScroll == 0 {
		t.Error("scroll stayed at 0 after moving past the visible window")
	}
	if m.folderCursor >= m.folderScroll+m.folderRows()+1 {
		t.Errorf("cursor %d is outside the window starting at %d", m.folderCursor, m.folderScroll)
	}

	view := m.View()
	if !strings.Contains(view, fmt.Sprintf("folder%02d", m.folderCursor)) {
		t.Error("view does not show the selected folder")
	}
}

func TestResizeClampsFolderScroll(t *testing.T) {
	m := loaded(t, 140, 16, sampleFolders(40), nil)
	for i := 0; i < 30; i++ {
		next, _ := m.Update(keyMsg("j"))
		m = next.(Model)
	}

	next, _ := m.Update(tea.WindowSizeMsg{Width: 140, Height: 60})
	m = next.(Model)

	if m.folderScroll > max(0, len(m.folders)-m.folderRows()) {
		t.Errorf("scroll = %d exceeds the maximum for the new height", m.folderScroll)
	}
	if m.selectedFolderName() == "" {
		t.Error("selected folder lost after resize")
	}
}

func TestFolderLineIndentation(t *testing.T) {
	m := loaded(t, 140, 40, []mail.Folder{
		{Name: "Work", Delimiter: '/'},
		{Name: "Work/Projects", Delimiter: '/'},
	}, nil)

	nested := m.folderLine(1, 40)
	if !strings.Contains(nested, "  Projects") {
		t.Errorf("nested line = %q, want two spaces before the leaf", nested)
	}
	if strings.Contains(nested, "Work/") {
		t.Errorf("nested line = %q, want the leaf only", nested)
	}
}

func TestFolderLineTruncates(t *testing.T) {
	m := loaded(t, 140, 40, []mail.Folder{
		{Name: "a folder name long enough to need truncation", Delimiter: '/'},
	}, nil)

	line := m.folderLine(0, 20)
	if !strings.Contains(line, "…") {
		t.Errorf("folder line %q has no ellipsis", line)
	}
	if got := lipgloss.Width(line); got > 20 {
		t.Errorf("folder line width = %d, want at most 20", got)
	}
}

func TestReloadKeyStartsLoad(t *testing.T) {
	m := loaded(t, 140, 40, sampleFolders(3), nil)

	next, cmd := m.Update(keyMsg("r"))
	if got := next.(Model).state; got != stateConnecting {
		t.Errorf("state after r = %v, want connecting", got)
	}
	if cmd == nil {
		t.Error("r returned no command")
	}
}

// selectedFolderName is a test helper that names the selected folder.
func (m Model) selectedFolderName() string {
	folder, ok := m.selectedFolder()
	if !ok {
		return ""
	}

	return folder.Name
}

// keyMsg builds a rune keypress message.
func keyMsg(key string) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)}
}
