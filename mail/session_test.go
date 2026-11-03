package mail

import (
	"errors"
	"strings"
	"testing"

	"github.com/emersion/go-imap/v2"
	"github.com/renderorange/chute/config"
)

// fakeClient stands in for a connected IMAP client in session tests.
type fakeClient struct {
	folders  []Folder
	messages []Message
	body     *Body
	err      error

	closed   bool
	folder   string
	uid      imap.UID
	requests int

	storeFolder string
	storeUIDs   []imap.UID
	storeAdd    bool
	storeFlags  []imap.Flag

	deleteFolder string
	deleteTrash  string
	deleteUIDs   []imap.UID
	deleteErr    error

	moveFolder string
	moveDest   string
	moveUIDs   []imap.UID
	moveErr    error

	purgeFolder string
	purgeUIDs   []imap.UID
	purgeErr    error

	emptyFolder string
	emptyErr    error

	compose     *Compose
	appendCalls []appendCall
	appendErr   error
	appendUID   imap.UID

	unseen     uint32
	unseenErr  error
	unseenSeen []string

	poll      PollResult
	pollErr   error
	pollSince imap.UID
	uids      []imap.UID
	uidsErr   error
}

// appendCall records one APPEND so a test can assert the folder, the flags and the
// bytes that went to the server.
type appendCall struct {
	folder string
	flags  []imap.Flag
	msg    []byte
}

func (f *fakeClient) Folders() ([]Folder, error) {
	f.requests++
	if f.err != nil {
		return nil, f.err
	}

	return f.folders, nil
}

func (f *fakeClient) FetchEnvelopes(folder string) ([]Message, error) {
	f.requests++
	f.folder = folder
	if f.err != nil {
		return nil, f.err
	}

	return f.messages, nil
}

func (f *fakeClient) FetchBody(folder string, uid imap.UID) (*Body, error) {
	f.requests++
	f.folder = folder
	f.uid = uid
	if f.err != nil {
		return nil, f.err
	}

	return f.body, nil
}

func (f *fakeClient) Close() error {
	f.closed = true

	return nil
}

// FetchCompose returns the canned compose value.
func (f *fakeClient) FetchCompose(folder string, uid imap.UID) (*Compose, error) {
	f.requests++
	f.folder = folder
	f.uid = uid

	if f.err != nil {
		return nil, f.err
	}

	return f.compose, nil
}

// Append records the folder, flags and bytes, and reports the canned UID.
func (f *fakeClient) Append(folder string, flags []imap.Flag, msg []byte) (imap.UID, bool, error) {
	f.requests++
	f.appendCalls = append(f.appendCalls, appendCall{folder: folder, flags: flags, msg: msg})

	if f.appendErr != nil {
		return 0, false, f.appendErr
	}
	if f.err != nil {
		return 0, false, f.err
	}

	return f.appendUID, f.appendUID != 0, nil
}

// Unseen records the folder it counted so a test can prove the session asked for
// the folder the caller named.
func (f *fakeClient) Unseen(folder string) (uint32, error) {
	f.requests++
	f.unseenSeen = append(f.unseenSeen, folder)

	if f.unseenErr != nil {
		return 0, f.unseenErr
	}
	if f.err != nil {
		return 0, f.err
	}

	return f.unseen, nil
}

// Poll returns the canned result and records the high-water mark it was given.
func (f *fakeClient) Poll(folder string, since imap.UID) (PollResult, error) {
	f.requests++
	f.folder = folder
	f.pollSince = since

	if f.pollErr != nil {
		return PollResult{}, f.pollErr
	}
	if f.err != nil {
		return PollResult{}, f.err
	}

	return f.poll, nil
}

// UIDs returns the canned UID list.
func (f *fakeClient) UIDs(folder string) ([]imap.UID, error) {
	f.requests++
	f.folder = folder

	if f.uidsErr != nil {
		return nil, f.uidsErr
	}
	if f.err != nil {
		return nil, f.err
	}

	return f.uids, nil
}

// DeleteMessages records the resolved trash mailbox so session tests can prove
// the role lookup ran against the unfiltered listing.
func (f *fakeClient) DeleteMessages(folder, trash string, uids []imap.UID) error {
	f.requests++
	f.deleteFolder = folder
	f.deleteTrash = trash
	f.deleteUIDs = uids

	if f.deleteErr != nil {
		return f.deleteErr
	}

	return f.err
}

// MoveToFolder records the destination a session resolved, so archive and junk
// tests can prove the role lookup ran and one UID set reached the client.
func (f *fakeClient) MoveToFolder(folder, dest string, uids []imap.UID) error {
	f.requests++
	f.moveFolder = folder
	f.moveDest = dest
	f.moveUIDs = uids

	if f.moveErr != nil {
		return f.moveErr
	}

	return f.err
}

// PurgeMessages records a permanent delete of named UIDs.
func (f *fakeClient) PurgeMessages(folder string, uids []imap.UID) error {
	f.requests++
	f.purgeFolder = folder
	f.purgeUIDs = uids

	if f.purgeErr != nil {
		return f.purgeErr
	}

	return f.err
}

// EmptyFolder records an explicit empty of one mailbox.
func (f *fakeClient) EmptyFolder(folder string) error {
	f.requests++
	f.emptyFolder = folder

	if f.emptyErr != nil {
		return f.emptyErr
	}

	return f.err
}

// StoreFlags records the call so session tests can assert one UID set reached
// the client unchanged.
func (f *fakeClient) StoreFlags(folder string, uids []imap.UID, add bool, flags ...imap.Flag) error {
	f.requests++
	f.storeFolder = folder
	f.storeUIDs = uids
	f.storeAdd = add
	f.storeFlags = flags

	return f.err
}

func TestSessionReusesConnection(t *testing.T) {
	connects := 0
	connect := func(config.Account) (sessionClient, error) {
		connects++
		return &fakeClient{folders: []Folder{{Name: "INBOX"}}}, nil
	}
	session := newSession(config.Account{Name: "fastmail"}, connect)

	for i := 0; i < 3; i++ {
		if _, err := session.Folders(); err != nil {
			t.Fatalf("Folders call %d: %v", i, err)
		}
	}

	if connects != 1 {
		t.Errorf("connect calls = %d, want 1", connects)
	}
	if err := session.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

func TestSessionReconnectsAfterFailure(t *testing.T) {
	connects := 0
	var first *fakeClient
	connect := func(config.Account) (sessionClient, error) {
		connects++
		client := &fakeClient{}
		if connects == 1 {
			client.err = errors.New("connection reset")
			first = client
		}

		return client, nil
	}
	session := newSession(config.Account{Name: "fastmail"}, connect)

	if _, err := session.Folders(); err == nil {
		t.Fatal("first Folders returned no error")
	}
	if first == nil || !first.closed {
		t.Error("the failed client was not closed")
	}

	if _, err := session.Folders(); err != nil {
		t.Fatalf("second Folders: %v", err)
	}
	if connects != 2 {
		t.Errorf("connect calls = %d, want 2", connects)
	}
	if err := session.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

func TestSessionCloseDisconnects(t *testing.T) {
	client := &fakeClient{}
	session := newSession(config.Account{Name: "fastmail"}, func(config.Account) (sessionClient, error) {
		return client, nil
	})

	if _, err := session.Folders(); err != nil {
		t.Fatalf("Folders: %v", err)
	}
	if err := session.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if !client.closed {
		t.Error("Close did not close the connected client")
	}
	if _, err := session.Folders(); !errors.Is(err, errSessionClosed) {
		t.Errorf("Folders after Close = %v, want errSessionClosed", err)
	}
}

func TestSessionPropagatesOperations(t *testing.T) {
	client := &fakeClient{
		messages: []Message{{UID: 7}},
		body:     &Body{Text: "hello"},
	}
	session := newSession(config.Account{Name: "fastmail"}, func(config.Account) (sessionClient, error) {
		return client, nil
	})
	defer session.Close()

	messages, err := session.FetchEnvelopes("INBOX")
	if err != nil {
		t.Fatalf("FetchEnvelopes: %v", err)
	}
	if len(messages) != 1 || messages[0].UID != 7 {
		t.Errorf("messages = %+v, want one with UID 7", messages)
	}
	if client.folder != "INBOX" {
		t.Errorf("folder = %q, want INBOX", client.folder)
	}

	body, err := session.FetchBody("INBOX", 7)
	if err != nil {
		t.Fatalf("FetchBody: %v", err)
	}
	if got, want := body.Text, "hello"; got != want {
		t.Errorf("body text = %q, want %q", got, want)
	}
	if client.uid != 7 {
		t.Errorf("uid = %d, want 7", client.uid)
	}
}

func TestManagerReusesSessionPerAccount(t *testing.T) {
	manager := NewManager()
	defer manager.Close()

	first := manager.For(config.Account{Name: "fastmail"})
	again := manager.For(config.Account{Name: "fastmail"})
	other := manager.For(config.Account{Name: "work"})

	if first != again {
		t.Error("the same account got a second session")
	}
	if first == other {
		t.Error("different accounts share a session")
	}
}

func TestManagerCloseClosesSessions(t *testing.T) {
	client := &fakeClient{}
	manager := &Manager{
		connect:  func(config.Account) (sessionClient, error) { return client, nil },
		sessions: make(map[string]*Session),
	}

	session := manager.For(config.Account{Name: "fastmail"})
	if _, err := session.Folders(); err != nil {
		t.Fatalf("Folders: %v", err)
	}
	if err := manager.Close(); err != nil {
		t.Fatalf("Manager.Close: %v", err)
	}
	if !client.closed {
		t.Error("Manager.Close did not close the account connection")
	}
}

func TestSessionHidesFolders(t *testing.T) {
	client := &fakeClient{folders: []Folder{
		{Name: "INBOX", Delimiter: '/'},
		{Name: "Notes", Delimiter: '/'},
		{Name: "Notes/Archive", Delimiter: '/'},
	}}
	acct := config.Account{Name: "fastmail", IMAP: config.IMAP{HideFolders: []string{"Notes"}}}
	session := newSession(acct, func(config.Account) (sessionClient, error) {
		return client, nil
	})
	defer session.Close()

	folders, err := session.Folders()
	if err != nil {
		t.Fatalf("Folders: %v", err)
	}
	if len(folders) != 1 || folders[0].Name != "INBOX" {
		t.Errorf("folders = %v, want only INBOX", folders)
	}
}

// TestSessionRoleFolderResolvesHiddenTrash proves Trash stays usable when the
// user hides it from the folder list. Delete would silently break if the role
// lookup read the filtered list.
func TestSessionRoleFolderResolvesHiddenTrash(t *testing.T) {
	client := &fakeClient{folders: []Folder{
		{Name: "INBOX", Delimiter: '/'},
		{Name: "Trash", Delimiter: '/', Attrs: []imap.MailboxAttr{imap.MailboxAttrTrash}},
	}}
	acct := config.Account{Name: "fastmail", IMAP: config.IMAP{HideFolders: []string{"Trash"}}}
	session := newSession(acct, func(config.Account) (sessionClient, error) {
		return client, nil
	})
	defer session.Close()

	folders, err := session.Folders()
	if err != nil {
		t.Fatalf("Folders: %v", err)
	}
	if len(folders) != 1 || folders[0].Name != "INBOX" {
		t.Fatalf("folders = %v, want Trash hidden", folders)
	}

	name, ok := session.RoleFolder(RoleTrash)
	if !ok {
		t.Fatal("RoleFolder(RoleTrash) found nothing; it read the filtered list")
	}
	if name != "Trash" {
		t.Errorf("trash folder = %q, want Trash", name)
	}
}

// TestSessionRoleFolderUsesOverride proves the config override beats the
// special-use attribute.
func TestSessionRoleFolderUsesOverride(t *testing.T) {
	client := &fakeClient{folders: []Folder{
		{Name: "Bin", Delimiter: '/'},
		{Name: "Trash", Delimiter: '/', Attrs: []imap.MailboxAttr{imap.MailboxAttrTrash}},
	}}
	acct := config.Account{Name: "fastmail", IMAP: config.IMAP{Trash: "Bin"}}
	session := newSession(acct, func(config.Account) (sessionClient, error) {
		return client, nil
	})
	defer session.Close()

	if _, err := session.Folders(); err != nil {
		t.Fatalf("Folders: %v", err)
	}

	name, ok := session.RoleFolder(RoleTrash)
	if !ok {
		t.Fatal("RoleFolder(RoleTrash) found nothing")
	}
	if name != "Bin" {
		t.Errorf("trash folder = %q, want the configured Bin", name)
	}
}

// TestSessionRoleFolderBeforeListing proves the role lookup never opens its own
// connection or issues its own LIST mid-action.
func TestSessionRoleFolderBeforeListing(t *testing.T) {
	connects := 0
	session := newSession(config.Account{Name: "fastmail"}, func(config.Account) (sessionClient, error) {
		connects++

		return &fakeClient{}, nil
	})
	defer session.Close()

	if _, ok := session.RoleFolder(RoleTrash); ok {
		t.Error("RoleFolder(RoleTrash) succeeded with no folder listing")
	}
	if connects != 0 {
		t.Errorf("connect calls = %d, want 0; RoleFolder must not connect", connects)
	}
}

// TestSessionRoleFolderMissing proves a server without Trash reports nothing
// rather than inventing one.
func TestSessionRoleFolderMissing(t *testing.T) {
	client := &fakeClient{folders: []Folder{{Name: "INBOX", Delimiter: '/'}}}
	session := newSession(config.Account{Name: "fastmail"}, func(config.Account) (sessionClient, error) {
		return client, nil
	})
	defer session.Close()

	if _, err := session.Folders(); err != nil {
		t.Fatalf("Folders: %v", err)
	}

	if name, ok := session.RoleFolder(RoleTrash); ok {
		t.Errorf("RoleFolder(RoleTrash) = %q, want not found", name)
	}
}

// TestSessionRoleFolderRefreshesOnReload proves a reload updates the role cache,
// so a mailbox renamed between reloads is not remembered forever.
func TestSessionRoleFolderRefreshesOnReload(t *testing.T) {
	client := &fakeClient{folders: []Folder{{Name: "INBOX", Delimiter: '/'}}}
	session := newSession(config.Account{Name: "fastmail"}, func(config.Account) (sessionClient, error) {
		return client, nil
	})
	defer session.Close()

	if _, err := session.Folders(); err != nil {
		t.Fatalf("first Folders: %v", err)
	}
	if _, ok := session.RoleFolder(RoleTrash); ok {
		t.Fatal("RoleFolder(RoleTrash) found a trash folder before one existed")
	}

	client.folders = []Folder{{Name: "Trash", Delimiter: '/', Attrs: []imap.MailboxAttr{imap.MailboxAttrTrash}}}
	if _, err := session.Folders(); err != nil {
		t.Fatalf("second Folders: %v", err)
	}

	name, ok := session.RoleFolder(RoleTrash)
	if !ok || name != "Trash" {
		t.Errorf("RoleFolder(RoleTrash) = %q, %v; want Trash, true", name, ok)
	}
}

// TestSessionStoreFlagsSendsOneCommand proves a bulk selection reaches the
// client as one call with every UID intact.
func TestSessionStoreFlagsSendsOneCommand(t *testing.T) {
	client := &fakeClient{}
	session := newSession(config.Account{Name: "fastmail"}, func(config.Account) (sessionClient, error) {
		return client, nil
	})
	defer session.Close()

	if err := session.StoreFlags("INBOX", []imap.UID{4, 9, 15}, true, imap.FlagSeen); err != nil {
		t.Fatalf("StoreFlags: %v", err)
	}

	if client.requests != 1 {
		t.Errorf("client calls = %d, want 1", client.requests)
	}
	if client.storeFolder != "INBOX" {
		t.Errorf("folder = %q, want INBOX", client.storeFolder)
	}
	if len(client.storeUIDs) != 3 {
		t.Errorf("uids = %v, want three", client.storeUIDs)
	}
	if !client.storeAdd {
		t.Error("add = false, want true")
	}
	if len(client.storeFlags) != 1 || client.storeFlags[0] != imap.FlagSeen {
		t.Errorf("flags = %v, want [\\Seen]", client.storeFlags)
	}
}

// TestSessionStoreFlagsDropsConnectionOnError proves a failed write resets the
// connection, matching the read path.
func TestSessionStoreFlagsDropsConnectionOnError(t *testing.T) {
	client := &fakeClient{err: errors.New("store failed")}
	session := newSession(config.Account{Name: "fastmail"}, func(config.Account) (sessionClient, error) {
		return client, nil
	})
	defer session.Close()

	if err := session.StoreFlags("INBOX", []imap.UID{1}, true, imap.FlagSeen); err == nil {
		t.Fatal("StoreFlags returned no error")
	}
	if !client.closed {
		t.Error("the failed client was not closed")
	}
}

// TestSessionDeleteResolvesTrash proves delete finds Trash even when the user
// hides it from the folder list.
func TestSessionDeleteResolvesTrash(t *testing.T) {
	client := &fakeClient{folders: []Folder{
		{Name: "INBOX", Delimiter: '/'},
		{Name: "Trash", Delimiter: '/', Attrs: []imap.MailboxAttr{imap.MailboxAttrTrash}},
	}}
	acct := config.Account{Name: "fastmail", IMAP: config.IMAP{HideFolders: []string{"Trash"}}}
	session := newSession(acct, func(config.Account) (sessionClient, error) {
		return client, nil
	})
	defer session.Close()

	if _, err := session.Folders(); err != nil {
		t.Fatalf("Folders: %v", err)
	}

	if err := session.Delete("INBOX", []imap.UID{7, 8}); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	if client.deleteFolder != "INBOX" {
		t.Errorf("source = %q, want INBOX", client.deleteFolder)
	}
	if client.deleteTrash != "Trash" {
		t.Errorf("trash = %q, want the resolved Trash folder", client.deleteTrash)
	}
	if len(client.deleteUIDs) != 2 {
		t.Errorf("uids = %v, want two", client.deleteUIDs)
	}
}

// TestSessionDeleteRefusesInsideTrash proves permanent delete is not reachable by
// accident: the same key inside Trash does nothing.
func TestSessionDeleteRefusesInsideTrash(t *testing.T) {
	client := &fakeClient{folders: []Folder{
		{Name: "INBOX", Delimiter: '/'},
		{Name: "Trash", Delimiter: '/', Attrs: []imap.MailboxAttr{imap.MailboxAttrTrash}},
	}}
	session := newSession(config.Account{Name: "fastmail"}, func(config.Account) (sessionClient, error) {
		return client, nil
	})
	defer session.Close()

	if _, err := session.Folders(); err != nil {
		t.Fatalf("Folders: %v", err)
	}
	requests := client.requests

	err := session.Delete("trash", []imap.UID{1})
	if err == nil {
		t.Fatal("Delete inside Trash returned no error")
	}
	if !strings.Contains(err.Error(), "already the trash folder") {
		t.Errorf("error = %q, want it to name the trash folder", err)
	}
	if client.requests != requests {
		t.Error("the refusal still reached the server")
	}
}

// TestSessionDeleteWithoutTrash proves a missing trash role is reported before
// anything is attempted.
func TestSessionDeleteWithoutTrash(t *testing.T) {
	client := &fakeClient{folders: []Folder{{Name: "INBOX", Delimiter: '/'}}}
	session := newSession(config.Account{Name: "fastmail"}, func(config.Account) (sessionClient, error) {
		return client, nil
	})
	defer session.Close()

	if _, err := session.Folders(); err != nil {
		t.Fatalf("Folders: %v", err)
	}

	err := session.Delete("INBOX", []imap.UID{1})
	if err == nil {
		t.Fatal("Delete without a trash folder returned no error")
	}
	if !strings.Contains(err.Error(), "trash role") {
		t.Errorf("error = %q, want it to name the missing role", err)
	}
}

// TestSessionDeleteDropsConnectionOnError proves a failed delete resets the
// connection.
func TestSessionDeleteDropsConnectionOnError(t *testing.T) {
	client := &fakeClient{
		folders: []Folder{
			{Name: "INBOX", Delimiter: '/'},
			{Name: "Trash", Delimiter: '/', Attrs: []imap.MailboxAttr{imap.MailboxAttrTrash}},
		},
		deleteErr: errors.New("delete failed"),
	}
	session := newSession(config.Account{Name: "fastmail"}, func(config.Account) (sessionClient, error) {
		return client, nil
	})
	defer session.Close()

	if _, err := session.Folders(); err != nil {
		t.Fatalf("Folders: %v", err)
	}
	if err := session.Delete("INBOX", []imap.UID{1}); err == nil {
		t.Fatal("Delete returned no error")
	}
	if !client.closed {
		t.Error("the failed client was not closed")
	}
}

// TestDeleteInTrashIsRecognisable proves the refusal is a typed error, so the TUI can
// render "already in Trash" instead of leaking "mail: account ..." into the status bar.
func TestDeleteInTrashIsRecognisable(t *testing.T) {
	client := &fakeClient{}
	session := newSession(config.Account{Name: "memory", IMAP: config.IMAP{Trash: "Trash"}},
		func(config.Account) (sessionClient, error) { return client, nil })
	session.folders = []Folder{{Name: "Trash"}}

	err := session.Delete("Trash", []imap.UID{1})
	if err == nil {
		t.Fatal("Delete accepted the trash folder as its source")
	}
	if !errors.Is(err, ErrAlreadyInTrash) {
		t.Errorf("error %v does not match ErrAlreadyInTrash", err)
	}
}
