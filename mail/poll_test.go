package mail

import (
	"errors"
	"strings"
	"testing"

	"github.com/emersion/go-imap/v2"
	"github.com/renderorange/chute/config"
)

// TestPollReturnsOnlyMessagesNewerThanSince proves the incremental fetch returns
// the messages after the caller's high-water mark and nothing before it.
func TestPollReturnsOnlyMessagesNewerThanSince(t *testing.T) {
	server := newTestServer(t, testCaps())
	client := server.connect(t)

	first := server.appendMessage(t, "INBOX", "one")
	server.appendMessage(t, "INBOX", "two")
	server.appendMessage(t, "INBOX", "three")

	result, err := client.Poll("INBOX", first)
	if err != nil {
		t.Fatalf("Poll: %v", err)
	}
	if len(result.Messages) != 2 {
		t.Fatalf("Messages = %d, want 2", len(result.Messages))
	}
	for _, msg := range result.Messages {
		if msg.UID <= first {
			t.Errorf("Poll returned UID %d, which is not newer than %d", msg.UID, first)
		}
	}
	if result.Total != 3 {
		t.Errorf("Total = %d, want 3", result.Total)
	}
}

// TestPollWithNothingNewReturnsNoMessages pins the IMAP quirk: UID FETCH n:* returns
// the highest-UID message even when n exceeds it, so a quiet poll must filter it out
// rather than re-announce the last message.
func TestPollWithNothingNewReturnsNoMessages(t *testing.T) {
	server := newTestServer(t, testCaps())
	client := server.connect(t)

	only := server.appendMessage(t, "INBOX", "the only message")

	result, err := client.Poll("INBOX", only)
	if err != nil {
		t.Fatalf("Poll: %v", err)
	}
	if len(result.Messages) != 0 {
		t.Errorf("Messages = %v, want none: nothing is newer than %d", result.Messages, only)
	}
	if result.Total != 1 {
		t.Errorf("Total = %d, want 1", result.Total)
	}
}

// TestPollReportsUnseenCount proves the poll carries the folder's unread count.
func TestPollReportsUnseenCount(t *testing.T) {
	server := newTestServer(t, testCaps())
	client := server.connect(t)

	read := server.appendMessage(t, "INBOX", "read")
	server.appendMessage(t, "INBOX", "unread")

	if err := client.StoreFlags("INBOX", []imap.UID{read}, true, imap.FlagSeen); err != nil {
		t.Fatalf("StoreFlags: %v", err)
	}

	result, err := client.Poll("INBOX", 0)
	if err != nil {
		t.Fatalf("Poll: %v", err)
	}
	if result.Unseen != 1 {
		t.Errorf("Unseen = %d, want 1", result.Unseen)
	}
}

// TestPollOnAnEmptyFolder proves an empty mailbox is a quiet poll, not an error.
func TestPollOnAnEmptyFolder(t *testing.T) {
	server := newTestServer(t, testCaps())
	client := server.connect(t)

	result, err := client.Poll("INBOX", 0)
	if err != nil {
		t.Fatalf("Poll: %v", err)
	}
	if len(result.Messages) != 0 || result.Total != 0 || result.Unseen != 0 {
		t.Errorf("Poll = %+v, want an empty result", result)
	}
}

// TestPollCostsOneFetchAndOneStatus is the gate the issue asks for: one incremental
// UID FETCH and one STATUS, and no sequence-number whole-folder FETCH.
func TestPollCostsOneFetchAndOneStatus(t *testing.T) {
	server := newTestServer(t, testCaps())
	client := server.connect(t)

	first := server.appendMessage(t, "INBOX", "one")
	server.appendMessage(t, "INBOX", "two")

	if _, err := client.Poll("INBOX", first); err != nil {
		t.Fatalf("Poll: %v", err)
	}

	if got := countClientCommands(server.log, "UID"); got != 1 {
		t.Errorf("UID commands = %d, want 1", got)
	}
	if got := countClientCommands(server.log, "STATUS"); got != 1 {
		t.Errorf("STATUS commands = %d, want 1", got)
	}
	// A sequence-number FETCH is the whole-folder command the issue forbids.
	if got := countClientCommands(server.log, "FETCH"); got != 0 {
		t.Errorf("whole-folder FETCH commands = %d, want 0", got)
	}
}

// TestPollCarriesFlagsAndEnvelope proves the merged data is usable by the list: a
// polled message carries the headers and flags the row renderer reads.
func TestPollCarriesFlagsAndEnvelope(t *testing.T) {
	server := newTestServer(t, testCaps())
	client := server.connect(t)

	server.appendThreadedMessage(t, "INBOX", "polled subject", "<polled@example.org>", "")

	result, err := client.Poll("INBOX", 0)
	if err != nil {
		t.Fatalf("Poll: %v", err)
	}
	if len(result.Messages) != 1 {
		t.Fatalf("Messages = %d, want 1", len(result.Messages))
	}

	msg := result.Messages[0]
	if msg.Subject() != "polled subject" {
		t.Errorf("Subject = %q, want %q", msg.Subject(), "polled subject")
	}
	// go-imap's ENVELOPE drops the angle brackets, so match on the id itself.
	if !strings.Contains(msg.MessageID, "polled@example.org") {
		t.Errorf("MessageID = %q, want the polled id", msg.MessageID)
	}
	if msg.Seen() {
		t.Error("a freshly appended message should not carry \\Seen")
	}
}

// TestUIDsListsEveryMessage proves the reconcile path can name every surviving UID.
func TestUIDsListsEveryMessage(t *testing.T) {
	server := newTestServer(t, testCaps())
	client := server.connect(t)

	first := server.appendMessage(t, "INBOX", "one")
	second := server.appendMessage(t, "INBOX", "two")

	uids, err := client.UIDs("INBOX")
	if err != nil {
		t.Fatalf("UIDs: %v", err)
	}
	if len(uids) != 2 {
		t.Fatalf("UIDs = %v, want two", uids)
	}
	if uids[0] != first || uids[1] != second {
		t.Errorf("UIDs = %v, want %d and %d", uids, first, second)
	}
}

// TestSessionPollReusesTheConnection proves a poll runs on the account's persistent
// session rather than opening a connection per tick.
func TestSessionPollReusesTheConnection(t *testing.T) {
	connects := 0
	connect := func(config.Account) (sessionClient, error) {
		connects++

		return &fakeClient{poll: PollResult{Total: 2, Unseen: 1}}, nil
	}
	session := newSession(config.Account{Name: "fastmail"}, connect)

	for i := 0; i < 2; i++ {
		result, err := session.Poll("INBOX", 7)
		if err != nil {
			t.Fatalf("Poll call %d: %v", i, err)
		}
		if result.Total != 2 || result.Unseen != 1 {
			t.Errorf("Poll = %+v, want Total 2 and Unseen 1", result)
		}
	}

	if connects != 1 {
		t.Errorf("connect calls = %d, want 1", connects)
	}
}

// TestSessionPollReconnectsAfterAFailure proves a failed poll drops the connection so
// the next tick reconnects, the same discipline as every other session operation.
func TestSessionPollReconnectsAfterAFailure(t *testing.T) {
	connects := 0
	connect := func(config.Account) (sessionClient, error) {
		connects++

		return &fakeClient{pollErr: errors.New("SELECT failed")}, nil
	}
	session := newSession(config.Account{Name: "fastmail"}, connect)

	for i := 0; i < 2; i++ {
		if _, err := session.Poll("INBOX", 0); err == nil {
			t.Fatalf("Poll call %d: want an error", i)
		}
	}

	if connects != 2 {
		t.Errorf("connect calls = %d, want 2: a failed poll must drop the connection", connects)
	}
}

// TestSessionPollReportsAConnectFailure proves a poll on an unreachable account
// returns the connection error rather than an empty result.
func TestSessionPollReportsAConnectFailure(t *testing.T) {
	connect := func(config.Account) (sessionClient, error) {
		return nil, errors.New("dial failed")
	}
	session := newSession(config.Account{Name: "fastmail"}, connect)

	if _, err := session.Poll("INBOX", 0); err == nil {
		t.Fatal("Poll on an unreachable account returned no error")
	}
}

// TestSessionPollPassesTheHighWaterMark proves the session hands the client the
// mark the caller named, which is what keeps a poll incremental.
func TestSessionPollPassesTheHighWaterMark(t *testing.T) {
	fake := &fakeClient{poll: PollResult{Total: 1}}
	connect := func(config.Account) (sessionClient, error) {
		return fake, nil
	}
	session := newSession(config.Account{Name: "fastmail"}, connect)

	if _, err := session.Poll("INBOX", 42); err != nil {
		t.Fatalf("Poll: %v", err)
	}
	if fake.pollSince != 42 {
		t.Errorf("client saw since = %d, want 42", fake.pollSince)
	}
}

// TestSessionUIDsReusesTheConnection proves the reconcile fetch runs on the
// account's persistent session.
func TestSessionUIDsReusesTheConnection(t *testing.T) {
	connects := 0
	connect := func(config.Account) (sessionClient, error) {
		connects++

		return &fakeClient{uids: []imap.UID{1, 2}}, nil
	}
	session := newSession(config.Account{Name: "fastmail"}, connect)

	uids, err := session.UIDs("INBOX")
	if err != nil {
		t.Fatalf("UIDs: %v", err)
	}
	if len(uids) != 2 {
		t.Errorf("UIDs = %v, want two", uids)
	}
	if connects != 1 {
		t.Errorf("connect calls = %d, want 1", connects)
	}
}

// TestSessionUIDsReconnectsAfterAFailure proves a failed reconcile fetch drops the
// connection so the next poll reconnects.
func TestSessionUIDsReconnectsAfterAFailure(t *testing.T) {
	connects := 0
	connect := func(config.Account) (sessionClient, error) {
		connects++

		return &fakeClient{uidsErr: errors.New("FETCH failed")}, nil
	}
	session := newSession(config.Account{Name: "fastmail"}, connect)

	for i := 0; i < 2; i++ {
		if _, err := session.UIDs("INBOX"); err == nil {
			t.Fatalf("UIDs call %d: want an error", i)
		}
	}

	if connects != 2 {
		t.Errorf("connect calls = %d, want 2: a failed fetch must drop the connection", connects)
	}
}

// TestSessionUIDsReportsAConnectFailure proves the reconcile path reports an
// unreachable account instead of returning an empty set, which would drop every row.
func TestSessionUIDsReportsAConnectFailure(t *testing.T) {
	connect := func(config.Account) (sessionClient, error) {
		return nil, errors.New("dial failed")
	}
	session := newSession(config.Account{Name: "fastmail"}, connect)

	if _, err := session.UIDs("INBOX"); err == nil {
		t.Fatal("UIDs on an unreachable account returned no error")
	}
}
