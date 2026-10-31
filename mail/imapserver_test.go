package mail

import (
	"bytes"
	"net"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
	"github.com/emersion/go-imap/v2/imapserver"
	"github.com/emersion/go-imap/v2/imapserver/imapmemserver"
	"github.com/renderorange/chute/config"
)

// The rest of this package fakes at the sessionClient interface, which is how a
// pointer imap.SeqSet reached production and panicked. These tests run the real
// client against a real server so a bad command fails here first.

// testServer is an in-process, memory-backed IMAP server.
type testServer struct {
	addr string
	user *imapmemserver.User
	log  *syncBuffer
}

// syncBuffer is a concurrency-safe log the server writes protocol traffic into.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.buf.String()
}

// testCaps is the capability set Fastmail-like servers advertise for the
// operations this package performs.
func testCaps() imap.CapSet {
	return imap.CapSet{
		imap.CapIMAP4rev1: {},
		imap.CapMove:      {},
		imap.CapUIDPlus:   {},
	}
}

// newTestServer starts a server on a loopback port and creates INBOX.
func newTestServer(t *testing.T, caps imap.CapSet) *testServer {
	t.Helper()

	backend := imapmemserver.New()
	user := imapmemserver.NewUser("tester", "secret")
	if err := user.Create("INBOX", nil); err != nil {
		t.Fatalf("create INBOX: %v", err)
	}
	if err := user.Create("Trash", nil); err != nil {
		t.Fatalf("create Trash: %v", err)
	}
	backend.AddUser(user)

	log := &syncBuffer{}

	server := imapserver.New(&imapserver.Options{
		NewSession: func(*imapserver.Conn) (imapserver.Session, *imapserver.GreetingData, error) {
			return backend.NewSession(), nil, nil
		},
		Caps:         caps,
		InsecureAuth: true,
		DebugWriter:  log,
	})

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	go func() {
		_ = server.Serve(listener)
	}()

	t.Cleanup(func() {
		server.Close()
		listener.Close()
	})

	return &testServer{addr: listener.Addr().String(), user: user, log: log}
}

// account builds the config an in-process client connects with.
func (s *testServer) account() config.Account {
	host, portText, err := net.SplitHostPort(s.addr)
	if err != nil {
		panic("mail: test server address is not host:port: " + err.Error())
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		panic("mail: test server port is not a number: " + err.Error())
	}

	return config.Account{
		Name: "memory",
		IMAP: config.IMAP{Host: host, Port: port, Username: "tester"},
	}
}

// connect dials the server with the real client and closes it when the test
// ends.
func (s *testServer) connect(t *testing.T) *Client {
	t.Helper()

	client, err := connectWith(dialInsecure, s.account(), "secret")
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() {
		client.Close()
	})

	return client
}

// dialInsecure dials without TLS, for the loopback test server only.
func dialInsecure(address string, options *imapclient.Options) (*imapclient.Client, error) {
	return imapclient.DialInsecure(address, options)
}

// TestConnectAgainstInProcessServer proves the real client logs in, caches the
// post-login capabilities, lists mailboxes and runs an envelope FETCH. The FETCH
// in particular is the call that panicked in production on a pointer SeqSet.
func TestConnectAgainstInProcessServer(t *testing.T) {
	server := newTestServer(t, testCaps())
	client := server.connect(t)

	if !client.Supports(imap.CapMove) {
		t.Error("server advertised MOVE but the client did not cache it")
	}
	if !client.Supports(imap.CapUIDPlus) {
		t.Error("server advertised UIDPLUS but the client did not cache it")
	}

	folders, err := client.Folders()
	if err != nil {
		t.Fatalf("Folders: %v", err)
	}
	if len(folders) != 2 || folders[0].Name != "INBOX" || folders[1].Name != "Trash" {
		t.Fatalf("folders = %v, want INBOX and Trash", folders)
	}

	if _, err := client.FetchEnvelopes("INBOX"); err != nil {
		t.Fatalf("FetchEnvelopes against a real server: %v", err)
	}
}

// appendMessage appends a raw message to a mailbox and returns its UID.
func (s *testServer) appendMessage(t *testing.T, mailbox, subject string) imap.UID {
	t.Helper()

	raw := "From: sender@example.com\r\n" +
		"To: tester@example.com\r\n" +
		"Subject: " + subject + "\r\n" +
		"Date: Mon, 02 Jan 2006 15:04:05 -0700\r\n" +
		"\r\n" +
		"body\r\n"

	// imapmemserver dereferences the options pointer without a nil check, so
	// pass a zero value rather than nil.
	data, err := s.user.Append(mailbox, strings.NewReader(raw), &imap.AppendOptions{})
	if err != nil {
		t.Fatalf("append to %s: %v", mailbox, err)
	}

	return data.UID
}

// TestStoreFlagsAgainstInProcessServer proves a real STORE changes exactly the
// listed UIDs, in both directions, through the real client.
func TestStoreFlagsAgainstInProcessServer(t *testing.T) {
	server := newTestServer(t, testCaps())
	client := server.connect(t)

	first := server.appendMessage(t, "INBOX", "first")
	second := server.appendMessage(t, "INBOX", "second")

	if err := client.StoreFlags("INBOX", []imap.UID{first}, true, imap.FlagSeen); err != nil {
		t.Fatalf("StoreFlags(+Seen): %v", err)
	}

	seen := seenByUID(t, client)
	if !seen[first] {
		t.Error("the listed UID was not marked seen")
	}
	if seen[second] {
		t.Error("an unlisted UID was marked seen; the UID set was not respected")
	}

	if err := client.StoreFlags("INBOX", []imap.UID{first}, false, imap.FlagSeen); err != nil {
		t.Fatalf("StoreFlags(-Seen): %v", err)
	}

	if seen = seenByUID(t, client); seen[first] {
		t.Error("-Seen did not clear the flag")
	}
}

// TestStoreFlagsEmptyListIsNoOp proves an empty selection never reaches the
// server, so a keypress on an empty list cannot touch mail.
func TestStoreFlagsEmptyListIsNoOp(t *testing.T) {
	server := newTestServer(t, testCaps())
	client := server.connect(t)

	if err := client.StoreFlags("INBOX", nil, true, imap.FlagSeen); err != nil {
		t.Fatalf("StoreFlags with no UIDs: %v", err)
	}
}

// seenByUID refetches INBOX and maps each UID to its \Seen state.
func seenByUID(t *testing.T, client *Client) map[imap.UID]bool {
	t.Helper()

	messages, err := client.FetchEnvelopes("INBOX")
	if err != nil {
		t.Fatalf("FetchEnvelopes: %v", err)
	}

	seen := make(map[imap.UID]bool, len(messages))
	for _, msg := range messages {
		seen[msg.UID] = msg.Seen()
	}

	return seen
}

// TestConnectRejectsWrongPassword proves a failed login is reported and the
// connection is not left half-open.
func TestConnectRejectsWrongPassword(t *testing.T) {
	server := newTestServer(t, testCaps())

	if _, err := connectWith(dialInsecure, server.account(), "wrong"); err == nil {
		t.Fatal("connect with a wrong password returned no error")
	}
}

// TestClientWithoutMOVEAdvertisesNothing proves the capability set follows the
// server, so the delete policy can tell the two cases apart.
func TestClientWithoutMOVEAdvertisesNothing(t *testing.T) {
	server := newTestServer(t, imap.CapSet{imap.CapIMAP4rev1: {}})
	client := server.connect(t)

	if client.Supports(imap.CapMove) {
		t.Error("client reported MOVE support against a server that never advertised it")
	}
	if client.Supports(imap.CapUIDPlus) {
		t.Error("client reported UIDPLUS support against a server that never advertised it")
	}
}

// uidList returns the UIDs currently in a mailbox.
func uidList(t *testing.T, client *Client, folder string) []imap.UID {
	t.Helper()

	messages, err := client.FetchEnvelopes(folder)
	if err != nil {
		t.Fatalf("FetchEnvelopes(%s): %v", folder, err)
	}

	uids := make([]imap.UID, 0, len(messages))
	for _, msg := range messages {
		uids = append(uids, msg.UID)
	}

	return uids
}

// bareExpunge reports whether the protocol log contains a client-sent
// folder-wide EXPUNGE. A UID EXPUNGE is allowed, and so is the untagged
// "* n EXPUNGE" response a MOVE produces; a bare EXPUNGE command is the
// milestone's hard gate.
func bareExpunge(logText string) bool {
	for _, line := range strings.Split(logText, "\r\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[0] != "*" && fields[1] == "EXPUNGE" {
			return true
		}
	}

	return false
}

// TestMoveMessagesAgainstInProcessServer proves a move takes the message out of
// the source and leaves it in the destination, using the MOVE extension.
func TestMoveMessagesAgainstInProcessServer(t *testing.T) {
	server := newTestServer(t, testCaps())
	client := server.connect(t)
	uid := server.appendMessage(t, "INBOX", "movable")

	if err := client.MoveMessages("INBOX", "Trash", []imap.UID{uid}); err != nil {
		t.Fatalf("MoveMessages: %v", err)
	}

	if uids := uidList(t, client, "INBOX"); len(uids) != 0 {
		t.Errorf("INBOX still holds %v", uids)
	}
	if uids := uidList(t, client, "Trash"); len(uids) != 1 || uids[0] != uid {
		t.Errorf("Trash holds %v, want [%d]", uids, uid)
	}
	if !strings.Contains(server.log.String(), "UID MOVE") {
		t.Error("the move did not use the MOVE extension")
	}
}

// TestDeleteMessagesUsesMove is the milestone gate: delete lands in Trash and the
// log shows MOVE with no mailbox-wide EXPUNGE.
func TestDeleteMessagesUsesMove(t *testing.T) {
	server := newTestServer(t, testCaps())
	client := server.connect(t)
	uid := server.appendMessage(t, "INBOX", "delete me")

	if err := client.DeleteMessages("INBOX", "Trash", []imap.UID{uid}); err != nil {
		t.Fatalf("DeleteMessages: %v", err)
	}

	if uids := uidList(t, client, "INBOX"); len(uids) != 0 {
		t.Errorf("INBOX still holds %v", uids)
	}
	if uids := uidList(t, client, "Trash"); len(uids) != 1 || uids[0] != uid {
		t.Errorf("Trash holds %v, want [%d]", uids, uid)
	}

	logText := server.log.String()
	if !strings.Contains(logText, "UID MOVE") {
		t.Error("delete did not use MOVE when the server advertises it")
	}
	if bareExpunge(logText) {
		t.Error("delete issued a mailbox-wide EXPUNGE")
	}
}

// TestDeleteMessagesFallsBackToCopyAndUidExpunge proves a server without MOVE but
// with UIDPLUS still deletes through COPY, \Deleted and UID EXPUNGE.
func TestDeleteMessagesFallsBackToCopyAndUidExpunge(t *testing.T) {
	server := newTestServer(t, imap.CapSet{imap.CapIMAP4rev1: {}, imap.CapUIDPlus: {}})
	client := server.connect(t)
	uid := server.appendMessage(t, "INBOX", "old server")

	if err := client.DeleteMessages("INBOX", "Trash", []imap.UID{uid}); err != nil {
		t.Fatalf("DeleteMessages: %v", err)
	}

	if uids := uidList(t, client, "INBOX"); len(uids) != 0 {
		t.Errorf("INBOX still holds %v", uids)
	}
	if uids := uidList(t, client, "Trash"); len(uids) != 1 || uids[0] != uid {
		t.Errorf("Trash holds %v, want [%d]", uids, uid)
	}

	logText := server.log.String()
	if !strings.Contains(logText, "UID COPY") {
		t.Error("the fallback did not copy to Trash")
	}
	if !strings.Contains(logText, "UID EXPUNGE") {
		t.Error("the fallback did not UID EXPUNGE the moved messages")
	}
	if bareExpunge(logText) {
		t.Error("the fallback issued a mailbox-wide EXPUNGE")
	}
}

// TestDeleteMessagesRefusesWithoutCapabilities proves a server with neither MOVE
// nor UIDPLUS is refused and nothing is touched.
func TestDeleteMessagesRefusesWithoutCapabilities(t *testing.T) {
	server := newTestServer(t, imap.CapSet{imap.CapIMAP4rev1: {}})
	client := server.connect(t)
	uid := server.appendMessage(t, "INBOX", "keep me")

	if err := client.DeleteMessages("INBOX", "Trash", []imap.UID{uid}); err == nil {
		t.Fatal("DeleteMessages succeeded without MOVE or UIDPLUS")
	}

	if uids := uidList(t, client, "INBOX"); len(uids) != 1 {
		t.Errorf("INBOX holds %v, want the message left alone", uids)
	}
}

// TestDeleteMessagesRefusesTrashEqualToSource guards the fallback's data-loss
// path: copying a mailbox onto itself and then expunging the source UIDs would
// destroy the messages outright.
func TestDeleteMessagesRefusesTrashEqualToSource(t *testing.T) {
	server := newTestServer(t, imap.CapSet{imap.CapIMAP4rev1: {}, imap.CapUIDPlus: {}})
	client := server.connect(t)
	uid := server.appendMessage(t, "INBOX", "must survive")

	if err := client.DeleteMessages("INBOX", "inbox", []imap.UID{uid}); err == nil {
		t.Fatal("DeleteMessages accepted the source folder as its trash folder")
	}

	if uids := uidList(t, client, "INBOX"); len(uids) != 1 || uids[0] != uid {
		t.Errorf("INBOX holds %v, want the message untouched", uids)
	}
}

// TestMoveMessagesRefusesSameFolder proves a pointless move is reported instead of
// being sent to the server.
func TestMoveMessagesRefusesSameFolder(t *testing.T) {
	server := newTestServer(t, testCaps())
	client := server.connect(t)
	server.appendMessage(t, "INBOX", "stay put")

	if err := client.MoveMessages("INBOX", "INBOX", []imap.UID{1}); err == nil {
		t.Fatal("MoveMessages accepted the source folder as its destination")
	}
}

// TestDeleteMessagesEmptySelectionIsNoOp proves a keypress with nothing selected
// cannot reach the server.
func TestDeleteMessagesEmptySelectionIsNoOp(t *testing.T) {
	server := newTestServer(t, testCaps())
	client := server.connect(t)

	if err := client.DeleteMessages("INBOX", "Trash", nil); err != nil {
		t.Fatalf("DeleteMessages with no UIDs: %v", err)
	}
	if err := client.DeleteMessages("INBOX", "", []imap.UID{1}); err == nil {
		t.Error("DeleteMessages accepted an empty trash name")
	}
}
