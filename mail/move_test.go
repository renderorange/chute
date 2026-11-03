package mail

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/emersion/go-imap/v2"
	"github.com/renderorange/chute/config"
)

// createMailbox makes an extra mailbox on the in-process server, so the move tests
// can target a destination other than Trash.
func (s *testServer) createMailbox(t *testing.T, name string) {
	t.Helper()

	if err := s.user.Create(name, nil); err != nil {
		t.Fatalf("create %s: %v", name, err)
	}
}

// TestMoveToFolderUsesMove proves the archive path takes the MOVE extension and
// leaves the source, exactly like delete.
func TestMoveToFolderUsesMove(t *testing.T) {
	server := newTestServer(t, testCaps())
	server.createMailbox(t, "Archive")
	client := server.connect(t)
	uid := server.appendMessage(t, "INBOX", "archive me")

	if err := client.MoveToFolder("INBOX", "Archive", []imap.UID{uid}); err != nil {
		t.Fatalf("MoveToFolder: %v", err)
	}

	if uids := uidList(t, client, "INBOX"); len(uids) != 0 {
		t.Errorf("INBOX still holds %v", uids)
	}
	if uids := uidList(t, client, "Archive"); len(uids) != 1 || uids[0] != uid {
		t.Errorf("Archive holds %v, want [%d]", uids, uid)
	}

	logText := server.log.String()
	if !strings.Contains(logText, "UID MOVE") {
		t.Error("the archive move did not use MOVE")
	}
	if bareExpunge(logText) {
		t.Error("the archive move issued a mailbox-wide EXPUNGE")
	}
}

// TestMoveToFolderFallsBackToCopyAndUidExpunge proves a server without MOVE but with
// UIDPLUS still archives through COPY, \Deleted and UID EXPUNGE.
func TestMoveToFolderFallsBackToCopyAndUidExpunge(t *testing.T) {
	server := newTestServer(t, imap.CapSet{imap.CapIMAP4rev1: {}, imap.CapUIDPlus: {}})
	server.createMailbox(t, "Archive")
	client := server.connect(t)
	uid := server.appendMessage(t, "INBOX", "old server")

	if err := client.MoveToFolder("INBOX", "Archive", []imap.UID{uid}); err != nil {
		t.Fatalf("MoveToFolder: %v", err)
	}

	if uids := uidList(t, client, "INBOX"); len(uids) != 0 {
		t.Errorf("INBOX still holds %v", uids)
	}
	if uids := uidList(t, client, "Archive"); len(uids) != 1 || uids[0] != uid {
		t.Errorf("Archive holds %v, want [%d]", uids, uid)
	}

	logText := server.log.String()
	if !strings.Contains(logText, "UID COPY") {
		t.Error("the fallback did not copy to Archive")
	}
	if !strings.Contains(logText, "UID EXPUNGE") {
		t.Error("the fallback did not UID EXPUNGE the moved messages")
	}
	if bareExpunge(logText) {
		t.Error("the fallback issued a mailbox-wide EXPUNGE")
	}
}

// TestMoveToFolderRefusesWithoutCapabilities is the issue's hard gate: a server with
// neither MOVE nor UIDPLUS must not fall back to a folder-wide EXPUNGE.
func TestMoveToFolderRefusesWithoutCapabilities(t *testing.T) {
	server := newTestServer(t, imap.CapSet{imap.CapIMAP4rev1: {}})
	server.createMailbox(t, "Archive")
	client := server.connect(t)
	uid := server.appendMessage(t, "INBOX", "keep me")

	if err := client.MoveToFolder("INBOX", "Archive", []imap.UID{uid}); err == nil {
		t.Fatal("MoveToFolder succeeded without MOVE or UIDPLUS")
	}

	if uids := uidList(t, client, "INBOX"); len(uids) != 1 || uids[0] != uid {
		t.Errorf("INBOX holds %v, want the message left alone", uids)
	}
	if uids := uidList(t, client, "Archive"); len(uids) != 0 {
		t.Errorf("Archive holds %v, want nothing", uids)
	}

	logText := server.log.String()
	if strings.Contains(logText, "UID MOVE") || strings.Contains(logText, "UID COPY") {
		t.Error("the refused move still reached the server")
	}
	if bareExpunge(logText) {
		t.Error("the refused move issued a mailbox-wide EXPUNGE")
	}
}

// TestMoveToFolderRefusesSameFolder guards the fallback's data-loss path.
func TestMoveToFolderRefusesSameFolder(t *testing.T) {
	server := newTestServer(t, testCaps())
	client := server.connect(t)
	uid := server.appendMessage(t, "INBOX", "stay put")

	if err := client.MoveToFolder("INBOX", "inbox", []imap.UID{uid}); err == nil {
		t.Fatal("MoveToFolder accepted the source folder as its destination")
	}

	if uids := uidList(t, client, "INBOX"); len(uids) != 1 || uids[0] != uid {
		t.Errorf("INBOX holds %v, want the message untouched", uids)
	}
}

// TestMoveToFolderEmptySelectionIsNoOp proves a keypress with nothing selected cannot
// reach the server.
func TestMoveToFolderEmptySelectionIsNoOp(t *testing.T) {
	server := newTestServer(t, testCaps())
	server.createMailbox(t, "Archive")
	client := server.connect(t)

	if err := client.MoveToFolder("INBOX", "Archive", nil); err != nil {
		t.Fatalf("MoveToFolder with no messages: %v", err)
	}

	if strings.Contains(server.log.String(), "UID MOVE") {
		t.Error("an empty selection still sent a MOVE")
	}
}

// newMoveSession builds a session over fakeClient with the test's account settings.
func newMoveSession(t *testing.T, acct config.Account, client *fakeClient) *Session {
	t.Helper()

	session := newSession(acct, func(config.Account) (sessionClient, error) { return client, nil })
	t.Cleanup(func() { _ = session.Close() })

	return session
}

// TestSessionMoveToResolvesTheRole proves archive resolves through the last listing
// and the client receives one destination and one UID set.
func TestSessionMoveToResolvesTheRole(t *testing.T) {
	client := &fakeClient{folders: []Folder{
		{Name: "INBOX", Delimiter: '/'},
		{Name: "Archive", Delimiter: '/', Attrs: []imap.MailboxAttr{imap.MailboxAttrArchive}},
	}}
	session := newMoveSession(t, config.Account{Name: "fastmail"}, client)

	if _, err := session.Folders(); err != nil {
		t.Fatalf("Folders: %v", err)
	}

	dest, err := session.MoveTo("INBOX", RoleArchive, []imap.UID{4, 5})
	if err != nil {
		t.Fatalf("MoveTo: %v", err)
	}
	if dest != "Archive" {
		t.Errorf("dest = %q, want Archive", dest)
	}
	if client.moveFolder != "INBOX" || client.moveDest != "Archive" {
		t.Errorf("client got %q -> %q, want INBOX -> Archive", client.moveFolder, client.moveDest)
	}
	if fmt.Sprint(client.moveUIDs) != "[4 5]" {
		t.Errorf("uids = %v, want [4 5]", client.moveUIDs)
	}
}

// TestSessionMoveToNamedFolderResolvesIt proves the arbitrary-destination path resolves
// against the last listing and hands one destination and one UID set to the client.
func TestSessionMoveToNamedFolderResolvesIt(t *testing.T) {
	client := &fakeClient{folders: []Folder{
		{Name: "INBOX", Delimiter: '/'},
		{Name: "Projects", Delimiter: '/'},
	}}
	session := newMoveSession(t, config.Account{Name: "fastmail"}, client)

	if _, err := session.Folders(); err != nil {
		t.Fatalf("Folders: %v", err)
	}

	if err := session.MoveToFolder("INBOX", "projects", []imap.UID{4, 5}); err != nil {
		t.Fatalf("MoveToFolder: %v", err)
	}
	if client.moveFolder != "INBOX" || client.moveDest != "Projects" {
		t.Errorf("client got %q -> %q, want INBOX -> Projects", client.moveFolder, client.moveDest)
	}
	if fmt.Sprint(client.moveUIDs) != "[4 5]" {
		t.Errorf("uids = %v, want [4 5]", client.moveUIDs)
	}
}

// TestSessionMoveToNamedFolderRefusesUnknown keeps a stale picker from reaching the
// server with a mailbox that no longer exists.
func TestSessionMoveToNamedFolderRefusesUnknown(t *testing.T) {
	client := &fakeClient{folders: []Folder{{Name: "INBOX", Delimiter: '/'}}}
	session := newMoveSession(t, config.Account{Name: "fastmail"}, client)

	if _, err := session.Folders(); err != nil {
		t.Fatalf("Folders: %v", err)
	}

	err := session.MoveToFolder("INBOX", "Nowhere", []imap.UID{1})
	if err == nil || !strings.Contains(err.Error(), "Nowhere") {
		t.Fatalf("error = %v, want a message naming the missing mailbox", err)
	}
	if client.moveFolder != "" {
		t.Error("the client was asked to move to a mailbox that is not in the listing")
	}
}

// TestSessionMoveToNamedFolderRefusesNoSelect covers the \Noselect flag: a container
// mailbox is not a destination.
func TestSessionMoveToNamedFolderRefusesNoSelect(t *testing.T) {
	client := &fakeClient{folders: []Folder{
		{Name: "INBOX", Delimiter: '/'},
		{Name: "All Mail", Delimiter: '/', Attrs: []imap.MailboxAttr{imap.MailboxAttrNoSelect}},
	}}
	session := newMoveSession(t, config.Account{Name: "fastmail"}, client)

	if _, err := session.Folders(); err != nil {
		t.Fatalf("Folders: %v", err)
	}

	err := session.MoveToFolder("INBOX", "All Mail", []imap.UID{1})
	if err == nil || !strings.Contains(err.Error(), "cannot hold messages") {
		t.Fatalf("error = %v, want the \\Noselect refusal", err)
	}
	if client.moveFolder != "" {
		t.Error("the client was asked to move into a \\Noselect mailbox")
	}
}

// TestSessionMoveToNamedFolderRefusesTheSource is the typed guard the UI maps to
// "already in <dest>".
func TestSessionMoveToNamedFolderRefusesTheSource(t *testing.T) {
	client := &fakeClient{folders: []Folder{{Name: "INBOX", Delimiter: '/'}}}
	session := newMoveSession(t, config.Account{Name: "fastmail"}, client)

	if _, err := session.Folders(); err != nil {
		t.Fatalf("Folders: %v", err)
	}

	err := session.MoveToFolder("inbox", "INBOX", []imap.UID{1})
	if !errors.Is(err, ErrAlreadyInRoleFolder) {
		t.Fatalf("error = %v, want ErrAlreadyInRoleFolder", err)
	}
	if client.moveFolder != "" {
		t.Error("the client was asked to move a folder into itself")
	}
}

// TestSessionMoveToJunkResolvesTheRole covers the second role rather than assuming the
// first one proves both.
func TestSessionMoveToJunkResolvesTheRole(t *testing.T) {
	client := &fakeClient{folders: []Folder{
		{Name: "INBOX", Delimiter: '/'},
		{Name: "Junk", Delimiter: '/', Attrs: []imap.MailboxAttr{imap.MailboxAttrJunk}},
	}}
	session := newMoveSession(t, config.Account{Name: "fastmail"}, client)

	if _, err := session.Folders(); err != nil {
		t.Fatalf("Folders: %v", err)
	}

	dest, err := session.MoveTo("INBOX", RoleJunk, []imap.UID{7})
	if err != nil {
		t.Fatalf("MoveTo: %v", err)
	}
	if dest != "Junk" {
		t.Errorf("dest = %q, want Junk", dest)
	}
}

// TestSessionMoveToPrefersTheConfiguredOverride is the issue's "config override wins
// over the special-use attribute".
func TestSessionMoveToPrefersTheConfiguredOverride(t *testing.T) {
	client := &fakeClient{folders: []Folder{
		{Name: "INBOX", Delimiter: '/'},
		{Name: "Server Archive", Delimiter: '/', Attrs: []imap.MailboxAttr{imap.MailboxAttrArchive}},
		{Name: "Old Mail", Delimiter: '/'},
	}}
	session := newMoveSession(t, config.Account{Name: "fastmail", IMAP: config.IMAP{Archive: "Old Mail"}}, client)

	if _, err := session.Folders(); err != nil {
		t.Fatalf("Folders: %v", err)
	}

	dest, err := session.MoveTo("INBOX", RoleArchive, []imap.UID{1})
	if err != nil {
		t.Fatalf("MoveTo: %v", err)
	}
	if dest != "Old Mail" {
		t.Errorf("dest = %q, want the configured Old Mail", dest)
	}
	if client.moveDest != "Old Mail" {
		t.Errorf("client dest = %q, want Old Mail", client.moveDest)
	}
}

// TestSessionMoveToWithoutARoleFolder is the issue's "refuse loudly": a typed error the
// status bar can turn into "no archive folder configured", and no server change.
func TestSessionMoveToWithoutARoleFolder(t *testing.T) {
	client := &fakeClient{folders: []Folder{{Name: "INBOX", Delimiter: '/'}}}
	session := newMoveSession(t, config.Account{Name: "fastmail"}, client)

	if _, err := session.Folders(); err != nil {
		t.Fatalf("Folders: %v", err)
	}

	dest, err := session.MoveTo("INBOX", RoleArchive, []imap.UID{1})
	if err == nil {
		t.Fatal("MoveTo succeeded without an archive folder")
	}
	if !errors.Is(err, ErrNoRoleFolder) {
		t.Errorf("error %v does not match ErrNoRoleFolder", err)
	}
	if dest != "" {
		t.Errorf("dest = %q, want none resolved", dest)
	}
	if client.moveFolder != "" {
		t.Error("the client was asked to move with no destination")
	}
}

// TestSessionMoveToTheSameFolderIsRecognisable is the archive analogue of
// ErrAlreadyInTrash: typed, and the destination is returned so the UI can name it.
func TestSessionMoveToTheSameFolderIsRecognisable(t *testing.T) {
	client := &fakeClient{folders: []Folder{
		{Name: "INBOX", Delimiter: '/'},
		{Name: "Archive", Delimiter: '/', Attrs: []imap.MailboxAttr{imap.MailboxAttrArchive}},
	}}
	session := newMoveSession(t, config.Account{Name: "fastmail"}, client)

	if _, err := session.Folders(); err != nil {
		t.Fatalf("Folders: %v", err)
	}

	dest, err := session.MoveTo("archive", RoleArchive, []imap.UID{1})
	if err == nil {
		t.Fatal("MoveTo accepted the archive folder as its source")
	}
	if !errors.Is(err, ErrAlreadyInRoleFolder) {
		t.Errorf("error %v does not match ErrAlreadyInRoleFolder", err)
	}
	if dest != "Archive" {
		t.Errorf("dest = %q, want Archive so the status can name it", dest)
	}
	if client.moveFolder != "" {
		t.Error("the client was asked to move a folder into itself")
	}
}

// TestSessionMoveToSeesAHiddenRoleFolder proves hide_folders hides a mailbox from the
// pane, not from role resolution.
func TestSessionMoveToSeesAHiddenRoleFolder(t *testing.T) {
	client := &fakeClient{folders: []Folder{
		{Name: "INBOX", Delimiter: '/'},
		{Name: "Archive", Delimiter: '/', Attrs: []imap.MailboxAttr{imap.MailboxAttrArchive}},
	}}
	session := newMoveSession(t, config.Account{
		Name: "fastmail",
		IMAP: config.IMAP{HideFolders: []string{"Archive"}},
	}, client)

	folders, err := session.Folders()
	if err != nil {
		t.Fatalf("Folders: %v", err)
	}
	if len(folders) != 1 {
		t.Fatalf("visible folders = %v, want Archive hidden", folders)
	}

	dest, err := session.MoveTo("INBOX", RoleArchive, []imap.UID{1})
	if err != nil {
		t.Fatalf("MoveTo: %v", err)
	}
	if dest != "Archive" {
		t.Errorf("dest = %q, want the hidden Archive", dest)
	}
}

// TestSessionMoveToDropsConnectionOnError proves a failed move resets the connection,
// the same as a failed delete.
func TestSessionMoveToDropsConnectionOnError(t *testing.T) {
	client := &fakeClient{
		folders: []Folder{
			{Name: "INBOX", Delimiter: '/'},
			{Name: "Archive", Delimiter: '/', Attrs: []imap.MailboxAttr{imap.MailboxAttrArchive}},
		},
		moveErr: errors.New("move failed"),
	}
	session := newMoveSession(t, config.Account{Name: "fastmail"}, client)

	if _, err := session.Folders(); err != nil {
		t.Fatalf("Folders: %v", err)
	}

	if _, err := session.MoveTo("INBOX", RoleArchive, []imap.UID{1}); err == nil {
		t.Fatal("MoveTo returned no error")
	}
	if !client.closed {
		t.Error("the failed client was not closed")
	}
}
