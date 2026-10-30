package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/renderorange/chute/mail"
)

func TestFoldersOverlayOpensInTwoColumnMode(t *testing.T) {
	m := loaded(t, 110, 30, sampleFolders(3), nil)

	next, _ := m.Update(keyMsg("\\"))
	m = next.(Model)

	if !m.foldersOpen {
		t.Fatal("\\ did not open the folders overlay")
	}
	view := m.View()
	for _, want := range []string{"folders", "enter:open"} {
		if !strings.Contains(view, want) {
			t.Errorf("view missing %q", want)
		}
	}
}

func TestFoldersOverlaySelectsFolder(t *testing.T) {
	m := loaded(t, 110, 30, []mail.Folder{
		{Name: "INBOX", Delimiter: '/'},
		{Name: "Work", Delimiter: '/'},
	}, nil)
	m.loadMessages = fakeMessages(sampleMessages(1), nil)

	next, _ := m.Update(keyMsg("\\"))
	m = next.(Model)
	next, _ = m.Update(keyMsg("j"))
	m = next.(Model)
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)

	if got, want := m.openFolder, "Work"; got != want {
		t.Errorf("openFolder = %q, want %q", got, want)
	}
	if m.foldersOpen {
		t.Error("overlay stayed open after selecting a folder")
	}
	if cmd == nil {
		t.Error("enter on a folder returned no command")
	}
}

func TestFoldersOverlayClosesOnEscape(t *testing.T) {
	m := loaded(t, 110, 30, sampleFolders(3), nil)

	next, _ := m.Update(keyMsg("\\"))
	m = next.(Model)

	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if next.(Model).foldersOpen {
		t.Error("esc did not close the folders overlay")
	}
}

func TestFoldersKeyFocusesPanelWhenDrawn(t *testing.T) {
	m := loaded(t, 140, 40, sampleFolders(3), nil)
	m.focus = focusMessages

	next, _ := m.Update(keyMsg("\\"))
	m = next.(Model)

	if got := m.focus; got != focusFolders {
		t.Errorf("focus = %v, want folders", got)
	}
	if m.foldersOpen {
		t.Error("three-column mode opened a folders overlay")
	}
}
