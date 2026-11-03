package tui

import (
	"testing"

	"github.com/emersion/go-imap/v2"
	"github.com/renderorange/chute/config"
	"github.com/renderorange/chute/mail"
)

// fakePurge records permanent deletes and empty-Trash calls.
type fakePurge struct {
	purgeCalls int
	emptyCalls int
	folder     string
	uids       []imap.UID
	purgeErr   error
	emptyErr   error
}

func (f *fakePurge) purge(_ config.Account, folder string, uids []imap.UID) error {
	f.purgeCalls++
	f.folder = folder
	f.uids = append([]imap.UID(nil), uids...)

	return f.purgeErr
}

func (f *fakePurge) empty(_ config.Account) error {
	f.emptyCalls++

	return f.emptyErr
}

// withTrash opens the trash mailbox with a recording permanent-delete loader.
func withTrash(t *testing.T, messages int) (Model, *fakePurge) {
	t.Helper()

	m := loaded(t, 140, 40, []mail.Folder{
		{Name: "INBOX", Delimiter: '/'},
		{Name: "Trash", Delimiter: '/', Attrs: []imap.MailboxAttr{imap.MailboxAttrTrash}},
	}, nil)
	m.loadMessages = fakeMessages(sampleMessages(messages), nil)

	next, cmd := m.openFolderByName("Trash", false)
	m = next.(Model)
	m = deliver(t, m, cmd)

	purge := &fakePurge{}
	m.purgeMessages = purge.purge
	m.emptyTrash = purge.empty

	return m, purge
}

// TestDeleteInsideTrashAsksToDeletePermanently replaces the old "already in Trash"
// dead end: d in Trash opens the harder confirmation and sends no command until it is
// answered.
func TestDeleteInsideTrashAsksToDeletePermanently(t *testing.T) {
	m, purge := withTrash(t, 3)

	selected, ok := m.selectedMessage()
	if !ok {
		t.Fatal("no message under the cursor")
	}

	m = press(t, m, keyMsg("d"))

	if !m.confirmOpen {
		t.Fatal("d in Trash opened no confirmation")
	}
	if m.confirmKind != confirmPurge {
		t.Errorf("confirm kind = %v, want confirmPurge", m.confirmKind)
	}
	if purge.purgeCalls != 0 {
		t.Error("a command was sent before the confirmation was answered")
	}

	m = press(t, m, keyMsg("enter"))

	if purge.purgeCalls != 1 {
		t.Fatalf("purge calls = %d, want 1", purge.purgeCalls)
	}
	if purge.folder != "Trash" || len(purge.uids) != 1 || purge.uids[0] != selected.UID {
		t.Errorf("purged %q %v, want Trash [%d]", purge.folder, purge.uids, selected.UID)
	}
	if len(m.rows) != 2 {
		t.Errorf("rows = %d, want 2", len(m.rows))
	}
	if want := "deleted 1 message permanently"; m.status != want {
		t.Errorf("status = %q, want %q", m.status, want)
	}
}

// TestEmptyTrashAsksAndEmpties covers X: the confirmation names the count and one call
// empties the folder.
func TestEmptyTrashAsksAndEmpties(t *testing.T) {
	m, purge := withTrash(t, 3)

	m = press(t, m, keyMsg("X"))

	if !m.confirmOpen || m.confirmKind != confirmEmptyTrash {
		t.Fatalf("X did not open the empty-Trash confirmation (open=%v kind=%v)", m.confirmOpen, m.confirmKind)
	}
	if m.pendingEmpty != 3 {
		t.Errorf("pendingEmpty = %d, want 3", m.pendingEmpty)
	}

	m = press(t, m, keyMsg("enter"))

	if purge.emptyCalls != 1 {
		t.Fatalf("empty calls = %d, want 1", purge.emptyCalls)
	}
	if len(m.rows) != 0 {
		t.Errorf("rows = %d, want the Trash empty", len(m.rows))
	}
	if want := "emptied Trash (3 messages)"; m.status != want {
		t.Errorf("status = %q, want %q", m.status, want)
	}
}

// TestEmptyTrashOutsideTrashSaysSo keeps X from becoming a hidden global command.
func TestEmptyTrashOutsideTrashSaysSo(t *testing.T) {
	m := openInbox(t, withInbox(t, sampleMessages(2), nil))

	m = press(t, m, keyMsg("X"))

	if m.confirmOpen {
		t.Error("X opened the empty-Trash confirmation outside Trash")
	}
	if want := "not in Trash"; m.status != want {
		t.Errorf("status = %q, want %q", m.status, want)
	}
}

// TestEmptyTrashWhenEmpty says so instead of asking a pointless question.
func TestEmptyTrashWhenEmpty(t *testing.T) {
	m, purge := withTrash(t, 0)

	m = press(t, m, keyMsg("X"))

	if m.confirmOpen {
		t.Error("X asked to empty an empty Trash")
	}
	if want := "Trash is empty"; m.status != want {
		t.Errorf("status = %q, want %q", m.status, want)
	}
	if purge.emptyCalls != 0 {
		t.Error("an empty Trash sent a command")
	}
}

// TestPurgeRefusalIsShort maps the server capability refusal to the short status rather
// than the wrapped protocol text.
func TestPurgeRefusalIsShort(t *testing.T) {
	m, purge := withTrash(t, 2)
	purge.purgeErr = mail.ErrNoSafePurge

	m = press(t, m, keyMsg("d"))
	m = press(t, m, keyMsg("enter"))

	if want := "server cannot delete permanently"; m.status != want {
		t.Errorf("status = %q, want %q", m.status, want)
	}
	if len(m.rows) != 2 {
		t.Errorf("rows = %d, want the list unchanged", len(m.rows))
	}
}

// TestPurgeConfirmationsFitTheFrame pins both new dialogs at the 60x20 minimum; the
// permanent-delete sentence is the longest confirmation in the app.
func TestPurgeConfirmationsFitTheFrame(t *testing.T) {
	for _, tc := range []struct {
		name string
		key  string
	}{
		{"permanent delete", "d"},
		{"empty Trash", "X"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := loaded(t, 60, 20, []mail.Folder{
				{Name: "INBOX", Delimiter: '/'},
				{Name: "Trash", Delimiter: '/', Attrs: []imap.MailboxAttr{imap.MailboxAttrTrash}},
			}, nil)
			m.loadMessages = fakeMessages(sampleMessages(2), nil)

			next, cmd := m.openFolderByName("Trash", false)
			m = next.(Model)
			m = deliver(t, m, cmd)

			m = press(t, m, keyMsg(tc.key))

			assertExactFrame(t, m, tc.name)
		})
	}
}

// TestPurgeResultForAnotherAccountIsIgnored proves a late permanent delete cannot empty
// another account's list.
func TestPurgeResultForAnotherAccountIsIgnored(t *testing.T) {
	m, _ := withTrash(t, 2)
	m.status = "untouched"

	next, _ := m.Update(purgeMsg{account: "other", folder: "Trash", uids: []imap.UID{1}})
	m = next.(Model)

	if m.status != "untouched" || len(m.rows) != 2 {
		t.Errorf("status=%q rows=%d, want untouched and 2 rows", m.status, len(m.rows))
	}
}
