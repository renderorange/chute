package mail

import (
	"errors"
	"strings"
	"testing"

	"github.com/emersion/go-imap/v2"
	"github.com/renderorange/chute/config"
)

// session connects a real client to the in-process server and wraps it in the
// persistent session the interface uses. Folder management goes through the session,
// not the client, because the refusals depend on the cached listing.
func (s *testServer) session(t *testing.T, acct config.Account) *Session {
	t.Helper()

	if acct.Name == "" {
		acct = s.account()
	}
	acct.IMAP.Host = s.account().IMAP.Host
	acct.IMAP.Port = s.account().IMAP.Port
	acct.IMAP.Username = s.account().IMAP.Username

	session := newSession(acct, func(acct config.Account) (sessionClient, error) {
		return connectWith(dialInsecure, acct, "secret")
	})
	t.Cleanup(func() {
		session.Close()
	})

	return session
}

// folderNames lists the mailboxes in a listing, for a readable failure message.
func folderNames(folders []Folder) []string {
	names := make([]string, 0, len(folders))
	for _, folder := range folders {
		names = append(names, folder.Name)
	}

	return names
}

// hasFolder reports whether a listing contains a mailbox by name.
func hasFolder(folders []Folder, name string) bool {
	for _, folder := range folders {
		if folder.Name == name {
			return true
		}
	}

	return false
}

// TestNewFolderCreatesNestedParents is the issue's "support nested" gate. Some servers
// create the parents themselves and some do not (the in-process server does not), so
// chute creates every missing ancestor before the leaf.
func TestNewFolderCreatesNestedParents(t *testing.T) {
	server := newTestServer(t, testCaps())
	session := server.session(t, config.Account{})

	if _, err := session.Folders(); err != nil {
		t.Fatalf("Folders: %v", err)
	}

	folders, err := session.NewFolder("Work/2026/Q1")
	if err != nil {
		t.Fatalf("NewFolder: %v", err)
	}

	for _, want := range []string{"Work", "Work/2026", "Work/2026/Q1"} {
		if !hasFolder(folders, want) {
			t.Errorf("listing %v is missing %q", folderNames(folders), want)
		}
	}
}

// TestNewFolderSkipsExistingParents proves an ancestor that already exists is not
// created twice: the second CREATE would be refused by the server.
func TestNewFolderSkipsExistingParents(t *testing.T) {
	server := newTestServer(t, testCaps())
	server.createMailbox(t, "Work")
	session := server.session(t, config.Account{})

	if _, err := session.Folders(); err != nil {
		t.Fatalf("Folders: %v", err)
	}

	folders, err := session.NewFolder("Work/2026")
	if err != nil {
		t.Fatalf("NewFolder: %v", err)
	}
	if !hasFolder(folders, "Work/2026") {
		t.Errorf("listing %v is missing Work/2026", folderNames(folders))
	}
}

// TestNewFolderAtTopLevel creates a plain mailbox with no parents to invent.
func TestNewFolderAtTopLevel(t *testing.T) {
	server := newTestServer(t, testCaps())
	session := server.session(t, config.Account{})

	if _, err := session.Folders(); err != nil {
		t.Fatalf("Folders: %v", err)
	}

	folders, err := session.NewFolder("Receipts")
	if err != nil {
		t.Fatalf("NewFolder: %v", err)
	}
	if !hasFolder(folders, "Receipts") {
		t.Errorf("listing %v is missing Receipts", folderNames(folders))
	}
}

// TestNewFolderRefusesExistingName keeps a case-insensitive duplicate out of the
// listing: IMAP mailbox names are matched case-insensitively by this interface
// everywhere else, so creating both "Work" and "work" would be two rows for one
// mailbox.
func TestNewFolderRefusesExistingName(t *testing.T) {
	server := newTestServer(t, testCaps())
	session := server.session(t, config.Account{})

	if _, err := session.Folders(); err != nil {
		t.Fatalf("Folders: %v", err)
	}

	_, err := session.NewFolder("inbox")
	if !errors.Is(err, ErrFolderExists) {
		t.Fatalf("NewFolder(inbox) = %v, want ErrFolderExists", err)
	}
}

// TestNewFolderRejectsMalformedNames covers the shapes a prompt can produce. None of
// them may reach CREATE.
func TestNewFolderRejectsMalformedNames(t *testing.T) {
	server := newTestServer(t, testCaps())
	session := server.session(t, config.Account{})

	if _, err := session.Folders(); err != nil {
		t.Fatalf("Folders: %v", err)
	}

	for _, name := range []string{"", "   ", "Work/", "/Work", "Work//2026", "Work/ /2026", "Work\x00"} {
		t.Run(name, func(t *testing.T) {
			if _, err := session.NewFolder(name); err == nil {
				t.Errorf("NewFolder(%q) succeeded, want a refusal", name)
			}
		})
	}
}

// TestMoveFolderCarriesSubfolders is the nesting gate for a move. The in-process server
// renames only the named mailbox, exactly like a server that ignores the RFC's inferior
// names rule, so the session has to repair the descendants itself.
func TestMoveFolderCarriesSubfolders(t *testing.T) {
	server := newTestServer(t, testCaps())
	server.createMailbox(t, "Work")
	server.createMailbox(t, "Work/2026")
	server.createMailbox(t, "Work/2026/Q1")
	server.createMailbox(t, "Archive")
	session := server.session(t, config.Account{})

	if _, err := session.Folders(); err != nil {
		t.Fatalf("Folders: %v", err)
	}

	dest, folders, err := session.MoveFolder("Work", "Archive")
	if err != nil {
		t.Fatalf("MoveFolder: %v", err)
	}
	if dest != "Archive/Work" {
		t.Errorf("destination = %q, want Archive/Work", dest)
	}

	for _, want := range []string{"Archive/Work", "Archive/Work/2026", "Archive/Work/2026/Q1"} {
		if !hasFolder(folders, want) {
			t.Errorf("listing %v is missing %q", folderNames(folders), want)
		}
	}
	for _, gone := range []string{"Work", "Work/2026", "Work/2026/Q1"} {
		if hasFolder(folders, gone) {
			t.Errorf("listing %v still holds %q", folderNames(folders), gone)
		}
	}
}

// TestMoveFolderToTopLevel drops a nested folder back to the root.
func TestMoveFolderToTopLevel(t *testing.T) {
	server := newTestServer(t, testCaps())
	server.createMailbox(t, "Archive")
	server.createMailbox(t, "Archive/2026")
	session := server.session(t, config.Account{})

	if _, err := session.Folders(); err != nil {
		t.Fatalf("Folders: %v", err)
	}

	dest, folders, err := session.MoveFolder("Archive/2026", "")
	if err != nil {
		t.Fatalf("MoveFolder: %v", err)
	}
	if dest != "2026" {
		t.Errorf("destination = %q, want 2026", dest)
	}
	if !hasFolder(folders, "2026") || hasFolder(folders, "Archive/2026") {
		t.Errorf("listing %v is wrong after the move", folderNames(folders))
	}
}

// TestMoveFolderRefusesOwnSubtree stops the cycle a move into a descendant would make.
func TestMoveFolderRefusesOwnSubtree(t *testing.T) {
	server := newTestServer(t, testCaps())
	server.createMailbox(t, "Work")
	server.createMailbox(t, "Work/2026")
	session := server.session(t, config.Account{})

	if _, err := session.Folders(); err != nil {
		t.Fatalf("Folders: %v", err)
	}

	if _, _, err := session.MoveFolder("Work", "Work/2026"); !errors.Is(err, ErrFolderCycle) {
		t.Fatalf("MoveFolder into own subtree = %v, want ErrFolderCycle", err)
	}
}

// TestMoveFolderRefusesExistingName reports the collision before the server does, so
// the message can name the folder that is in the way.
func TestMoveFolderRefusesExistingName(t *testing.T) {
	server := newTestServer(t, testCaps())
	server.createMailbox(t, "Work")
	server.createMailbox(t, "Archive")
	server.createMailbox(t, "Archive/Work")
	session := server.session(t, config.Account{})

	if _, err := session.Folders(); err != nil {
		t.Fatalf("Folders: %v", err)
	}

	if _, _, err := session.MoveFolder("Work", "Archive"); !errors.Is(err, ErrFolderExists) {
		t.Fatalf("MoveFolder onto an existing name = %v, want ErrFolderExists", err)
	}
}

// TestMoveFolderRefusesSameParent refuses a move that would not change the name.
func TestMoveFolderRefusesSameParent(t *testing.T) {
	server := newTestServer(t, testCaps())
	server.createMailbox(t, "Work")
	server.createMailbox(t, "Work/2026")
	session := server.session(t, config.Account{})

	if _, err := session.Folders(); err != nil {
		t.Fatalf("Folders: %v", err)
	}

	if _, _, err := session.MoveFolder("Work/2026", "Work"); !errors.Is(err, ErrFolderNoOp) {
		t.Fatalf("MoveFolder within its own parent = %v, want ErrFolderNoOp", err)
	}
}

// TestMoveFolderRefusesInbox refuses the one mailbox the protocol cannot rename
// cleanly: renaming INBOX moves its messages out and leaves an empty INBOX behind.
func TestMoveFolderRefusesInbox(t *testing.T) {
	server := newTestServer(t, testCaps())
	server.createMailbox(t, "Archive")
	session := server.session(t, config.Account{})

	if _, err := session.Folders(); err != nil {
		t.Fatalf("Folders: %v", err)
	}

	if _, _, err := session.MoveFolder("INBOX", "Archive"); !errors.Is(err, ErrFolderProtected) {
		t.Fatalf("MoveFolder(INBOX) = %v, want ErrFolderProtected", err)
	}
}

// TestMoveFolderRefusesOverrideNamedFolder protects the mapping that resolves a role.
// A folder the server marks with an attribute travels with the attribute through a
// rename; a name typed into the config does not, so that one is refused and the status
// names the key to remove.
func TestMoveFolderRefusesOverrideNamedFolder(t *testing.T) {
	server := newTestServer(t, testCaps())
	server.createMailbox(t, "Archive")
	session := server.session(t, config.Account{Name: "memory", IMAP: config.IMAP{Trash: "Trash"}})

	if _, err := session.Folders(); err != nil {
		t.Fatalf("Folders: %v", err)
	}

	_, _, err := session.MoveFolder("Trash", "Archive")
	if !errors.Is(err, ErrFolderProtected) {
		t.Fatalf("MoveFolder(override-named Trash) = %v, want ErrFolderProtected", err)
	}
	if !strings.Contains(err.Error(), "trash") {
		t.Errorf("error %q does not name the config key to change", err)
	}
}

// TestDeleteFolderRefusesNotEmpty is the issue's central refusal, and it carries the
// count so the status can say how much mail is in the way.
func TestDeleteFolderRefusesNotEmpty(t *testing.T) {
	server := newTestServer(t, testCaps())
	server.createMailbox(t, "Work")
	server.appendMessage(t, "Work", "keep me")
	session := server.session(t, config.Account{})

	if _, err := session.Folders(); err != nil {
		t.Fatalf("Folders: %v", err)
	}

	_, err := session.DeleteFolder("Work")
	if !errors.Is(err, ErrFolderNotEmpty) {
		t.Fatalf("DeleteFolder(non-empty) = %v, want ErrFolderNotEmpty", err)
	}

	var notEmpty *FolderNotEmptyError
	if !errors.As(err, &notEmpty) {
		t.Fatalf("error %v does not carry the message count", err)
	}
	if notEmpty.Count != 1 {
		t.Errorf("count = %d, want 1", notEmpty.Count)
	}
}

// TestDeleteFolderRefusesChildren stops a delete that would orphan or destroy a
// subtree, depending on how the server implements it.
func TestDeleteFolderRefusesChildren(t *testing.T) {
	server := newTestServer(t, testCaps())
	server.createMailbox(t, "Work")
	server.createMailbox(t, "Work/2026")
	server.createMailbox(t, "Work/2026/Q1")
	session := server.session(t, config.Account{})

	if _, err := session.Folders(); err != nil {
		t.Fatalf("Folders: %v", err)
	}

	_, err := session.DeleteFolder("Work")
	if !errors.Is(err, ErrFolderHasChildren) {
		t.Fatalf("DeleteFolder(with children) = %v, want ErrFolderHasChildren", err)
	}

	var hasChildren *FolderHasChildrenError
	if !errors.As(err, &hasChildren) {
		t.Fatalf("error %v does not carry the subfolder count", err)
	}
	if hasChildren.Count != 2 {
		t.Errorf("count = %d, want 2", hasChildren.Count)
	}
}

// TestDeleteFolderRemovesEmpty proves the happy path drops the mailbox and the listing.
func TestDeleteFolderRemovesEmpty(t *testing.T) {
	server := newTestServer(t, testCaps())
	server.createMailbox(t, "Work")
	session := server.session(t, config.Account{})

	if _, err := session.Folders(); err != nil {
		t.Fatalf("Folders: %v", err)
	}

	folders, err := session.DeleteFolder("Work")
	if err != nil {
		t.Fatalf("DeleteFolder: %v", err)
	}
	if hasFolder(folders, "Work") {
		t.Errorf("listing %v still holds Work", folderNames(folders))
	}
}

// TestDeleteFolderRefusesInbox keeps the account's primary mailbox on the server.
func TestDeleteFolderRefusesInbox(t *testing.T) {
	server := newTestServer(t, testCaps())
	session := server.session(t, config.Account{})

	if _, err := session.Folders(); err != nil {
		t.Fatalf("Folders: %v", err)
	}

	if _, err := session.DeleteFolder("INBOX"); !errors.Is(err, ErrFolderProtected) {
		t.Fatalf("DeleteFolder(INBOX) = %v, want ErrFolderProtected", err)
	}
}

// TestDeleteFolderRefusesOverrideNamedFolder is the delete half of the mapping rule.
func TestDeleteFolderRefusesOverrideNamedFolder(t *testing.T) {
	server := newTestServer(t, testCaps())
	session := server.session(t, config.Account{Name: "memory", IMAP: config.IMAP{Trash: "Trash"}})

	if _, err := session.Folders(); err != nil {
		t.Fatalf("Folders: %v", err)
	}

	if _, err := session.DeleteFolder("Trash"); !errors.Is(err, ErrFolderProtected) {
		t.Fatalf("DeleteFolder(override-named Trash) = %v, want ErrFolderProtected", err)
	}
}

// TestDeleteFolderRemovesAnEmptyContainer covers the \Noselect case: a container holds no
// mail, so the count is zero without a STATUS a server may refuse.
func TestDeleteFolderRemovesAnEmptyContainer(t *testing.T) {
	before := []Folder{
		{Name: "INBOX", Delimiter: '/'},
		{Name: "Work", Delimiter: '/', Attrs: []imap.MailboxAttr{imap.MailboxAttrNoSelect}},
	}
	client := &fakeClient{folders: before}
	session := newSession(config.Account{Name: "fake"}, func(config.Account) (sessionClient, error) {
		return client, nil
	})

	if _, err := session.Folders(); err != nil {
		t.Fatalf("Folders: %v", err)
	}

	if _, err := session.DeleteFolder("Work"); err != nil {
		t.Fatalf("DeleteFolder(container): %v", err)
	}
	if client.countedMailbox != "" {
		t.Errorf("STATUS was issued for %q, want none for a container", client.countedMailbox)
	}
	if client.deletedMailbox != "Work" {
		t.Errorf("deleted %q, want Work", client.deletedMailbox)
	}
}

// TestFolderOpsRefreshCachedListing proves a folder made in this session can be
// operated on again without a reload: the session's own listing, which every refusal
// reads, was updated by the operation.
func TestFolderOpsRefreshCachedListing(t *testing.T) {
	server := newTestServer(t, testCaps())
	session := server.session(t, config.Account{})

	if _, err := session.Folders(); err != nil {
		t.Fatalf("Folders: %v", err)
	}
	if _, err := session.NewFolder("Temp"); err != nil {
		t.Fatalf("NewFolder: %v", err)
	}

	if _, err := session.DeleteFolder("Temp"); err != nil {
		t.Fatalf("DeleteFolder after NewFolder: %v", err)
	}
}

// TestMoveFolderLeavesCascadedDescendantsAlone is the other half of the nesting fix: a
// server that already renamed the subtree must not be asked to rename it a second time,
// because the old names are gone and the repair would fail after a successful move.
func TestMoveFolderLeavesCascadedDescendantsAlone(t *testing.T) {
	before := []Folder{
		{Name: "INBOX", Delimiter: '/'},
		{Name: "Work", Delimiter: '/'},
		{Name: "Work/2026", Delimiter: '/'},
		{Name: "Archive", Delimiter: '/'},
	}
	after := []Folder{
		{Name: "INBOX", Delimiter: '/'},
		{Name: "Archive", Delimiter: '/'},
		{Name: "Archive/Work", Delimiter: '/'},
		{Name: "Archive/Work/2026", Delimiter: '/'},
	}

	client := &fakeClient{folders: before}
	session := newSession(config.Account{Name: "fake"}, func(config.Account) (sessionClient, error) {
		return client, nil
	})

	if _, err := session.Folders(); err != nil {
		t.Fatalf("Folders: %v", err)
	}

	// The next listing the session asks for is the one after the rename.
	client.foldersQueue = [][]Folder{after}

	if _, _, err := session.MoveFolder("Work", "Archive"); err != nil {
		t.Fatalf("MoveFolder: %v", err)
	}
	if len(client.renamedMailboxes) != 1 {
		t.Fatalf("renames = %v, want only the named mailbox", client.renamedMailboxes)
	}
}

// TestMoveFolderRepairsShallowestFirst pins the repair order: a child renamed before
// its new parent exists is refused by a server that checks the parent path.
func TestMoveFolderRepairsShallowestFirst(t *testing.T) {
	before := []Folder{
		{Name: "INBOX", Delimiter: '/'},
		{Name: "Work", Delimiter: '/'},
		{Name: "Work/2026", Delimiter: '/'},
		{Name: "Work/2026/Q1", Delimiter: '/'},
		{Name: "Archive", Delimiter: '/'},
	}

	// The server moved only the named mailbox, so both descendants are still under
	// the old name when the session lists again.
	client := &fakeClient{folders: before}
	session := newSession(config.Account{Name: "fake"}, func(config.Account) (sessionClient, error) {
		return client, nil
	})

	if _, err := session.Folders(); err != nil {
		t.Fatalf("Folders: %v", err)
	}

	client.foldersQueue = [][]Folder{{
		{Name: "INBOX", Delimiter: '/'},
		{Name: "Archive", Delimiter: '/'},
		{Name: "Archive/Work", Delimiter: '/'},
		{Name: "Work/2026", Delimiter: '/'},
		{Name: "Work/2026/Q1", Delimiter: '/'},
	}}

	if _, _, err := session.MoveFolder("Work", "Archive"); err != nil {
		t.Fatalf("MoveFolder: %v", err)
	}

	want := []renameCall{
		{from: "Work", to: "Archive/Work"},
		{from: "Work/2026", to: "Archive/Work/2026"},
		{from: "Work/2026/Q1", to: "Archive/Work/2026/Q1"},
	}
	if len(client.renamedMailboxes) != len(want) {
		t.Fatalf("renames = %v, want %v", client.renamedMailboxes, want)
	}
	for i := range want {
		if client.renamedMailboxes[i] != want[i] {
			t.Errorf("rename %d = %v, want %v", i, client.renamedMailboxes[i], want[i])
		}
	}
}
