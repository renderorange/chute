package mail

import (
	"errors"
	"strings"
	"testing"

	"github.com/emersion/go-imap/v2"
	"github.com/renderorange/chute/config"
)

// TestPurgeMessagesWithNoSelectionIsANoOp keeps an empty visual range from reaching the
// server.
func TestPurgeMessagesWithNoSelectionIsANoOp(t *testing.T) {
	server := newTestServer(t, testCaps())
	client := server.connect(t)

	if err := client.PurgeMessages("INBOX", nil); err != nil {
		t.Fatalf("PurgeMessages(nil): %v", err)
	}
	if strings.Contains(server.log.String(), "EXPUNGE") {
		t.Error("an empty purge still reached the server")
	}
}

// TestSessionPurgeResetsTheConnectionOnError proves a failed permanent delete drops the
// session, so the next operation reconnects instead of reusing a broken stream.
func TestSessionPurgeResetsTheConnectionOnError(t *testing.T) {
	client := &fakeClient{folders: []Folder{
		{Name: "INBOX", Delimiter: '/'},
		{Name: "Trash", Delimiter: '/', Attrs: []imap.MailboxAttr{imap.MailboxAttrTrash}},
	}}
	connects := 0
	session := newSession(config.Account{Name: "fastmail"}, func(config.Account) (sessionClient, error) {
		connects++

		return client, nil
	})
	t.Cleanup(func() { _ = session.Close() })

	if _, err := session.Folders(); err != nil {
		t.Fatalf("Folders: %v", err)
	}

	client.purgeErr = errors.New("connection lost")
	if err := session.Purge("Trash", []imap.UID{1}); err == nil {
		t.Fatal("Purge swallowed the protocol error")
	}

	if _, err := session.Folders(); err != nil {
		t.Fatalf("Folders after the failure: %v", err)
	}
	if connects != 2 {
		t.Errorf("connects = %d, want the failed session replaced", connects)
	}
}

// TestSessionEmptyTrashReportsAProtocolError keeps the empty path from reporting success
// when the server refused.
func TestSessionEmptyTrashReportsAProtocolError(t *testing.T) {
	client := &fakeClient{
		folders: []Folder{
			{Name: "INBOX", Delimiter: '/'},
			{Name: "Trash", Delimiter: '/', Attrs: []imap.MailboxAttr{imap.MailboxAttrTrash}},
		},
		emptyErr: errors.New("expunge refused"),
	}
	session := newMoveSession(t, config.Account{Name: "fastmail"}, client)

	if _, err := session.Folders(); err != nil {
		t.Fatalf("Folders: %v", err)
	}

	if err := session.EmptyTrash(); err == nil {
		t.Fatal("EmptyTrash swallowed the protocol error")
	}
}

// TestPurgeMessagesRemovesOnlyTheSelection is the permanent-delete gate: exactly the
// chosen UIDs go, the control message stays, and the command is UID EXPUNGE rather than
// a mailbox-wide expunge.
func TestPurgeMessagesRemovesOnlyTheSelection(t *testing.T) {
	server := newTestServer(t, testCaps())
	client := server.connect(t)

	first := server.appendMessage(t, "INBOX", "one")
	control := server.appendMessage(t, "INBOX", "two")
	third := server.appendMessage(t, "INBOX", "three")

	if err := client.PurgeMessages("INBOX", []imap.UID{first, third}); err != nil {
		t.Fatalf("PurgeMessages: %v", err)
	}

	uids := uidList(t, client, "INBOX")
	if len(uids) != 1 || uids[0] != control {
		t.Errorf("INBOX holds %v, want only the control %d", uids, control)
	}

	logText := server.log.String()
	if !strings.Contains(logText, "UID EXPUNGE") {
		t.Error("the purge did not use UID EXPUNGE")
	}
	if bareExpunge(logText) {
		t.Error("the purge issued a mailbox-wide EXPUNGE")
	}
}

// TestPurgeMessagesNeedsUIDPLUS refuses permanent deletion on a server that cannot name
// the UIDs to expunge, rather than falling back to a folder-wide EXPUNGE.
func TestPurgeMessagesNeedsUIDPLUS(t *testing.T) {
	server := newTestServer(t, imap.CapSet{imap.CapIMAP4rev1: {}})
	client := server.connect(t)
	uid := server.appendMessage(t, "INBOX", "keep me")

	err := client.PurgeMessages("INBOX", []imap.UID{uid})
	if !errors.Is(err, ErrNoSafePurge) {
		t.Fatalf("error = %v, want ErrNoSafePurge", err)
	}
	if uids := uidList(t, client, "INBOX"); len(uids) != 1 || uids[0] != uid {
		t.Errorf("INBOX holds %v, want the message left alone", uids)
	}
	if strings.Contains(server.log.String(), "EXPUNGE") {
		t.Error("the refused purge still reached the server")
	}
}

// TestEmptyFolderRemovesEverything is the explicit-empty path: a folder-wide EXPUNGE is
// allowed here because the user asked for the whole folder.
func TestEmptyFolderRemovesEverything(t *testing.T) {
	server := newTestServer(t, testCaps())
	client := server.connect(t)

	server.appendMessage(t, "INBOX", "one")
	server.appendMessage(t, "INBOX", "two")
	server.appendMessage(t, "INBOX", "three")

	if err := client.EmptyFolder("INBOX"); err != nil {
		t.Fatalf("EmptyFolder: %v", err)
	}

	if uids := uidList(t, client, "INBOX"); len(uids) != 0 {
		t.Errorf("INBOX holds %v, want empty", uids)
	}
	if !bareExpunge(server.log.String()) {
		t.Error("EmptyFolder did not issue a folder-wide EXPUNGE")
	}
}

// TestSessionPurgeOnlyRunsInTrash proves the session refuses a permanent delete outside
// the resolved trash mailbox, and resolves the role for the call that is allowed.
func TestSessionPurgeOnlyRunsInTrash(t *testing.T) {
	client := &fakeClient{folders: []Folder{
		{Name: "INBOX", Delimiter: '/'},
		{Name: "Trash", Delimiter: '/', Attrs: []imap.MailboxAttr{imap.MailboxAttrTrash}},
	}}
	session := newMoveSession(t, config.Account{Name: "fastmail"}, client)

	if _, err := session.Folders(); err != nil {
		t.Fatalf("Folders: %v", err)
	}

	if err := session.Purge("INBOX", []imap.UID{1}); err == nil {
		t.Fatal("Purge ran outside the trash mailbox")
	}
	if client.purgeFolder != "" {
		t.Error("the client was asked to purge INBOX")
	}

	if err := session.Purge("Trash", []imap.UID{2, 3}); err != nil {
		t.Fatalf("Purge in Trash: %v", err)
	}
	if client.purgeFolder != "Trash" || len(client.purgeUIDs) != 2 {
		t.Errorf("client got %q %v, want Trash [2 3]", client.purgeFolder, client.purgeUIDs)
	}
}

// TestSessionEmptyTrashResolvesTheRole covers the empty path and the missing-role
// refusal.
func TestSessionEmptyTrashResolvesTheRole(t *testing.T) {
	client := &fakeClient{folders: []Folder{
		{Name: "INBOX", Delimiter: '/'},
		{Name: "Deleted Items", Delimiter: '/', Attrs: []imap.MailboxAttr{imap.MailboxAttrTrash}},
	}}
	session := newMoveSession(t, config.Account{Name: "fastmail"}, client)

	if _, err := session.Folders(); err != nil {
		t.Fatalf("Folders: %v", err)
	}

	if err := session.EmptyTrash(); err != nil {
		t.Fatalf("EmptyTrash: %v", err)
	}
	if client.emptyFolder != "Deleted Items" {
		t.Errorf("emptied %q, want Deleted Items", client.emptyFolder)
	}

	noTrash := &fakeClient{folders: []Folder{{Name: "INBOX", Delimiter: '/'}}}
	other := newMoveSession(t, config.Account{Name: "fastmail"}, noTrash)
	if _, err := other.Folders(); err != nil {
		t.Fatalf("Folders: %v", err)
	}
	if err := other.EmptyTrash(); !errors.Is(err, ErrNoRoleFolder) {
		t.Errorf("error = %v, want ErrNoRoleFolder", err)
	}
}
