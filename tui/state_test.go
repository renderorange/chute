package tui

import (
	"errors"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/renderorange/chute/config"
	"github.com/renderorange/chute/mail"
)

// stateRecorder stands in for the state file: it records what the model persists.
type stateRecorder struct {
	calls int
	saved config.State
	err   error
}

// save is the writer handed to WithSelection.
func (r *stateRecorder) save(s config.State) error {
	r.calls++
	r.saved = s

	return r.err
}

// stateModel builds a model with a remembered selection and a recording writer.
func stateModel(t *testing.T, sel config.State, folders []mail.Folder) (Model, *stateRecorder) {
	t.Helper()

	rec := &stateRecorder{}
	m := sized(140, 40).WithSelection(sel, rec.save)
	m.loadFolders = fakeFolders(folders, nil)

	return m, rec
}

// startModel delivers the first folder load the way Init does.
func startModel(t *testing.T, m Model) Model {
	t.Helper()

	cmd := m.Init()
	if cmd == nil {
		t.Fatal("Init returned no command")
	}

	next, _ := m.Update(cmd())

	return next.(Model)
}

// archiveFolders is a listing with something to remember besides INBOX.
func archiveFolders() []mail.Folder {
	return []mail.Folder{{Name: "INBOX"}, {Name: "Archive"}}
}

// TestRememberedAccountIsSelected proves the account from the last run wins over the
// one marked default.
func TestRememberedAccountIsSelected(t *testing.T) {
	m, _ := stateModel(t, config.State{Account: "work"}, archiveFolders())

	if got := m.accountName(); got != "work" {
		t.Errorf("account = %q, want the remembered work", got)
	}
}

// TestUnknownRememberedAccountFallsBack proves a removed account is not an error: the
// default account is used, silently.
func TestUnknownRememberedAccountFallsBack(t *testing.T) {
	m, _ := stateModel(t, config.State{Account: "ghost"}, archiveFolders())

	if got := m.accountName(); got != "fastmail" {
		t.Errorf("account = %q, want the default fastmail", got)
	}
}

// TestRememberedFolderOpensAtStartup is the issue's headline criterion: the folder
// open in the last run is opened with no keypress.
func TestRememberedFolderOpensAtStartup(t *testing.T) {
	sel := config.State{Account: "fastmail", Folders: map[string]string{"fastmail": "Archive"}}
	m, rec := stateModel(t, sel, archiveFolders())

	// Allow the configured start folder to be INBOX, so a remembered folder that is
	// ignored would show up as INBOX rather than as nothing.
	m.cfg.Accounts[m.account].StartFolder = nil

	m = startModel(t, m)

	if got := m.openFolder; got != "Archive" {
		t.Errorf("openFolder = %q, want the remembered Archive", got)
	}
	if rec.calls != 0 {
		t.Errorf("state written %d times at startup, want none: opening a remembered folder is not a change", rec.calls)
	}
}

// TestStaleRememberedFolderFallsBackSilently covers a folder renamed or hidden by
// hide_folders: the start folder opens and nothing is reported.
func TestStaleRememberedFolderFallsBackSilently(t *testing.T) {
	sel := config.State{Account: "fastmail", Folders: map[string]string{"fastmail": "Gone"}}
	m, rec := stateModel(t, sel, archiveFolders())
	m.cfg.Accounts[m.account].StartFolder = nil

	m = startModel(t, m)

	if got := m.openFolder; got != "INBOX" {
		t.Errorf("openFolder = %q, want the start folder INBOX", got)
	}
	if m.status != "" {
		t.Errorf("status = %q, want a stale remembered folder to be silent", m.status)
	}
	if rec.calls != 0 {
		t.Errorf("state written %d times, want none", rec.calls)
	}
}

// TestRememberedFolderForAnotherAccountIsIgnored proves the lookup is per account:
// work's folder must not be opened for fastmail.
func TestRememberedFolderForAnotherAccountIsIgnored(t *testing.T) {
	sel := config.State{Account: "fastmail", Folders: map[string]string{"work": "Archive"}}
	m, _ := stateModel(t, sel, archiveFolders())
	m.cfg.Accounts[m.account].StartFolder = nil

	m = startModel(t, m)

	if got := m.openFolder; got != "INBOX" {
		t.Errorf("openFolder = %q, want INBOX: the remembered folder belongs to work", got)
	}
}

// TestAccountSwitchIsRemembered proves a switch is written when it happens, not at
// exit: a killed terminal must not lose it.
func TestAccountSwitchIsRemembered(t *testing.T) {
	m, rec := stateModel(t, config.State{Account: "fastmail"}, archiveFolders())
	m = startModel(t, m)

	m = openSelector(t, m)
	m.accountCursor = 1

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)

	if rec.calls != 1 {
		t.Fatalf("state written %d times, want once", rec.calls)
	}
	if rec.saved.Account != "work" {
		t.Errorf("saved account = %q, want work", rec.saved.Account)
	}
}

// TestFolderOpenIsRemembered proves the folder is written for the account that opened
// it, so the next start can restore it.
func TestFolderOpenIsRemembered(t *testing.T) {
	m, rec := stateModel(t, config.State{Account: "fastmail"}, archiveFolders())
	m = startModel(t, m)

	m.focus = focusFolders
	m.folderCursor = 1

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)

	if got := m.openFolder; got != "Archive" {
		t.Fatalf("openFolder = %q, want Archive", got)
	}
	if rec.calls != 1 {
		t.Fatalf("state written %d times, want once", rec.calls)
	}
	if rec.saved.Account != "fastmail" {
		t.Errorf("saved account = %q, want fastmail", rec.saved.Account)
	}
	if got := rec.saved.Folders["fastmail"]; got != "Archive" {
		t.Errorf("saved folder = %q, want Archive", got)
	}
}

// TestStateWriteFailureIsSilent is the issue's "a read-only $HOME ... must not stop the
// app or produce a modal": the folder still opens and nothing is reported.
func TestStateWriteFailureIsSilent(t *testing.T) {
	m, rec := stateModel(t, config.State{Account: "fastmail"}, archiveFolders())
	m = startModel(t, m)
	rec.err = errors.New("read-only file system")

	m.focus = focusFolders
	m.folderCursor = 1

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)

	if got := m.openFolder; got != "Archive" {
		t.Errorf("openFolder = %q, want the folder to open anyway", got)
	}
	if m.status != "" {
		t.Errorf("status = %q, want the write failure silent", m.status)
	}
}

// TestSelectionSurvivesARestart is the issue's headline criterion end to end: what the
// first run wrote is enough for a fresh model to land on the same account and folder
// with no keypress, and that restart must not rewrite anything.
func TestSelectionSurvivesARestart(t *testing.T) {
	first, rec := stateModel(t, config.State{}, archiveFolders())
	first = startModel(t, first)

	first.focus = focusFolders
	first.folderCursor = 1

	next, _ := first.Update(tea.KeyMsg{Type: tea.KeyEnter})
	first = next.(Model)

	if got := first.openFolder; got != "Archive" {
		t.Fatalf("first run opened %q, want Archive", got)
	}

	before := rec.calls

	// Restart with the state the first run wrote.
	second := sized(140, 40).WithSelection(rec.saved, rec.save)
	second.loadFolders = fakeFolders(archiveFolders(), nil)
	second = startModel(t, second)

	if got := second.accountName(); got != "fastmail" {
		t.Errorf("account = %q, want fastmail", got)
	}
	if got := second.openFolder; got != "Archive" {
		t.Errorf("openFolder = %q, want the remembered Archive with no keypress", got)
	}
	if rec.calls != before {
		t.Error("the restart wrote the state file, so an idle start is not read-only")
	}
}

// TestModelWithoutSelectionHasNoStore proves the model stays inert without a store,
// which is what keeps every other test in this package off the developer's real state
// file. Recording the selection in memory still happens; there is simply nothing to
// persist it to.
func TestModelWithoutSelectionHasNoStore(t *testing.T) {
	m := openInbox(t, withInbox(t, sampleMessages(2), nil))

	if m.saveState != nil {
		t.Error("a model built without WithSelection has a state writer")
	}
	if got := m.selection.Folders["fastmail"]; got != "INBOX" {
		t.Errorf("selection = %+v, want the opened folder recorded in memory", m.selection)
	}
}
