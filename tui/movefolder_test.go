package tui

import (
	"testing"

	"github.com/emersion/go-imap/v2"
	"github.com/renderorange/chute/config"
	"github.com/renderorange/chute/mail"
)

// fakeFolderMove records named moves so the picker can be tested without a server.
type fakeFolderMove struct {
	calls  int
	folder string
	dest   string
	uids   []imap.UID
	err    error
}

func (f *fakeFolderMove) load(_ config.Account, folder, dest string, uids []imap.UID) error {
	f.calls++
	f.folder = folder
	f.dest = dest
	f.uids = append([]imap.UID(nil), uids...)

	return f.err
}

// moveFolders is the listing the picker sees: a source, two selectable targets and one
// \Noselect mailbox that must not be offered.
func moveFolders() []mail.Folder {
	return []mail.Folder{
		{Name: "INBOX", Delimiter: '/'},
		{Name: "Archive", Delimiter: '/'},
		{Name: "All Mail", Delimiter: '/', Attrs: []imap.MailboxAttr{imap.MailboxAttrNoSelect}},
		{Name: "Projects", Delimiter: '/'},
	}
}

// withFolderMove opens INBOX with the multi-folder listing and a recording loader.
func withFolderMove(t *testing.T, messages int) (Model, *fakeFolderMove) {
	t.Helper()

	m := loaded(t, 140, 40, moveFolders(), nil)
	m.loadMessages = fakeMessages(sampleMessages(messages), nil)
	m = openInbox(t, m)

	move := &fakeFolderMove{}
	m.moveToFolder = move.load

	return m, move
}

func pickerNames(m Model) []string {
	names := make([]string, 0, len(m.moveFolders))
	for _, folder := range m.moveFolders {
		names = append(names, folder.Name)
	}

	return names
}

// TestMoveKeyOpensThePicker covers the headline: M offers every selectable mailbox
// except the one the messages are already in.
func TestMoveKeyOpensThePicker(t *testing.T) {
	m, _ := withFolderMove(t, 3)

	m = press(t, m, keyMsg("M"))

	if !m.moveOpen {
		t.Fatal("M did not open the move picker")
	}
	if got := pickerNames(m); len(got) != 2 || got[0] != "Archive" || got[1] != "Projects" {
		t.Errorf("picker = %v, want Archive and Projects", got)
	}
	if m.moveCursor != 0 {
		t.Errorf("cursor = %d, want the first target", m.moveCursor)
	}
}

// TestMovePickerMovesToTheChosenFolder proves the chosen destination, the source and the
// UID set reach the loader once, and the moved row leaves the list.
func TestMovePickerMovesToTheChosenFolder(t *testing.T) {
	m, move := withFolderMove(t, 3)

	selected, ok := m.selectedMessage()
	if !ok {
		t.Fatal("no message under the cursor")
	}

	m = press(t, m, keyMsg("M"))
	m = press(t, m, keyMsg("down")) // Archive -> Projects
	m = press(t, m, keyMsg("enter"))

	if move.calls != 1 {
		t.Fatalf("loader calls = %d, want 1", move.calls)
	}
	if move.folder != "INBOX" || move.dest != "Projects" {
		t.Errorf("moved %q -> %q, want INBOX -> Projects", move.folder, move.dest)
	}
	if len(move.uids) != 1 || move.uids[0] != selected.UID {
		t.Errorf("uids = %v, want [%d]", move.uids, selected.UID)
	}
	if m.moveOpen {
		t.Error("the picker stayed open after moving")
	}
	if len(m.rows) != 2 {
		t.Errorf("rows = %d, want 2 after the move", len(m.rows))
	}
	if want := "moved 1 message to Projects"; m.status != want {
		t.Errorf("status = %q, want %q", m.status, want)
	}
}

// TestMovePickerEscClosesWithoutMoving keeps esc meaning cancel, as in every other modal.
func TestMovePickerEscClosesWithoutMoving(t *testing.T) {
	m, move := withFolderMove(t, 3)

	m = press(t, m, keyMsg("M"))
	m = press(t, m, keyMsg("esc"))

	if m.moveOpen {
		t.Error("esc left the picker open")
	}
	if move.calls != 0 {
		t.Errorf("loader calls = %d, want 0", move.calls)
	}
}

// TestMoveKeyWithoutOtherFoldersSaysSo avoids opening an empty picker.
func TestMoveKeyWithoutOtherFoldersSaysSo(t *testing.T) {
	m := loaded(t, 140, 40, []mail.Folder{{Name: "INBOX", Delimiter: '/'}}, nil)
	m.loadMessages = fakeMessages(sampleMessages(2), nil)
	m = openInbox(t, m)

	m = press(t, m, keyMsg("M"))

	if m.moveOpen {
		t.Error("M opened a picker with no destination")
	}
	if want := "no other folders"; m.status != want {
		t.Errorf("status = %q, want %q", m.status, want)
	}
}

// TestMoveKeyOutsideTheListIsInert proves the triage guard still applies: M from the
// folders panel does nothing.
func TestMoveKeyOutsideTheListIsInert(t *testing.T) {
	m := loaded(t, 140, 40, moveFolders(), nil)
	m.focus = focusFolders

	m = press(t, m, keyMsg("M"))

	if m.moveOpen {
		t.Error("M opened the picker outside the message list")
	}
}

// TestMoveResultForAnotherAccountIsIgnored proves a late move cannot rewrite another
// account's list or status.
func TestMoveResultForAnotherAccountIsIgnored(t *testing.T) {
	m, _ := withFolderMove(t, 3)
	m.status = "untouched"

	next, _ := m.Update(folderMoveMsg{account: "other", folder: "INBOX", dest: "Projects", uids: []imap.UID{1}})
	m = next.(Model)

	if m.status != "untouched" {
		t.Errorf("status = %q, want untouched", m.status)
	}
	if len(m.rows) != 3 {
		t.Errorf("rows = %d, want the list unchanged", len(m.rows))
	}
}

// TestFolderMoveStatusNamesTheDestination covers the typed refusal a stale picker can
// still produce, and keeps the server's own text for everything else.
func TestFolderMoveStatusNamesTheDestination(t *testing.T) {
	if got, want := folderMoveStatus(folderMoveMsg{dest: "Projects", err: mail.ErrAlreadyInRoleFolder}), "already in Projects"; got != want {
		t.Errorf("status = %q, want %q", got, want)
	}
	if got, want := folderMoveStatus(folderMoveMsg{dest: "Projects"}), ""; got != want {
		t.Errorf("status for a nil error = %q, want %q", got, want)
	}
}

// TestMovePickerFitsTheFrame pins the modal geometry at the minimum terminal size.
func TestMovePickerFitsTheFrame(t *testing.T) {
	m := loaded(t, 60, 20, moveFolders(), nil)
	m.loadMessages = fakeMessages(sampleMessages(3), nil)
	m = openInboxAnyMode(t, m)

	m = press(t, m, keyMsg("M"))

	assertExactFrame(t, m, "move picker")
}
