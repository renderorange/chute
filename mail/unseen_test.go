package mail

import (
	"errors"
	"testing"

	"github.com/emersion/go-imap/v2"
	"github.com/renderorange/chute/config"
)

// TestUnseenCountsUnreadAgainstInProcessServer proves STATUS UNSEEN works through
// the real client and counts messages that do not carry \Seen.
func TestUnseenCountsUnreadAgainstInProcessServer(t *testing.T) {
	server := newTestServer(t, testCaps())
	client := server.connect(t)

	server.appendMessage(t, "INBOX", "unread one")
	server.appendMessage(t, "INBOX", "unread two")

	count, err := client.Unseen("INBOX")
	if err != nil {
		t.Fatalf("Unseen: %v", err)
	}
	if count != 2 {
		t.Errorf("Unseen = %d, want 2", count)
	}
}

// TestUnseenReportsAnEmptyFolder proves a folder with no mail reports zero rather
// than an error, so the selector can show 0 unread.
func TestUnseenReportsAnEmptyFolder(t *testing.T) {
	server := newTestServer(t, testCaps())
	client := server.connect(t)

	count, err := client.Unseen("INBOX")
	if err != nil {
		t.Fatalf("Unseen: %v", err)
	}
	if count != 0 {
		t.Errorf("Unseen = %d, want 0", count)
	}
}

// TestUnseenReportsAMissingFolder proves a failed STATUS surfaces an error instead
// of reporting zero.
func TestUnseenReportsAMissingFolder(t *testing.T) {
	server := newTestServer(t, testCaps())
	client := server.connect(t)

	if _, err := client.Unseen("NoSuchFolder"); err == nil {
		t.Fatal("Unseen on a missing folder returned no error")
	}
}

// TestUnseenCountsUnreadOnly proves a message carrying \Seen is not counted.
func TestUnseenCountsUnreadOnly(t *testing.T) {
	server := newTestServer(t, testCaps())
	client := server.connect(t)

	uid := server.appendMessage(t, "INBOX", "unread")

	if err := client.StoreFlags("INBOX", []imap.UID{uid}, true, imap.FlagSeen); err != nil {
		t.Fatalf("StoreFlags: %v", err)
	}

	count, err := client.Unseen("INBOX")
	if err != nil {
		t.Fatalf("Unseen: %v", err)
	}
	if count != 0 {
		t.Errorf("Unseen = %d, want 0 after marking the only message read", count)
	}
}

// TestSessionUnseenReportsAConnectFailure proves a count on an unreachable account
// returns the connection error rather than a zero count.
func TestSessionUnseenReportsAConnectFailure(t *testing.T) {
	connect := func(config.Account) (sessionClient, error) {
		return nil, errors.New("dial failed")
	}
	session := newSession(config.Account{Name: "fastmail"}, connect)

	if _, err := session.Unseen("INBOX"); err == nil {
		t.Fatal("Unseen on an unreachable account returned no error")
	}
}

// TestSessionUnseenReconnectsAfterAFailedCount proves a failed STATUS drops the
// connection so the next count reconnects, the same discipline as every other
// session operation.
func TestSessionUnseenReconnectsAfterAFailedCount(t *testing.T) {
	connects := 0
	connect := func(config.Account) (sessionClient, error) {
		connects++

		return &fakeClient{unseenErr: errors.New("STATUS failed")}, nil
	}
	session := newSession(config.Account{Name: "fastmail"}, connect)

	for i := 0; i < 2; i++ {
		if _, err := session.Unseen("INBOX"); err == nil {
			t.Fatalf("Unseen call %d: want an error", i)
		}
	}

	if connects != 2 {
		t.Errorf("connect calls = %d, want 2: a failed STATUS must drop the connection", connects)
	}
}

// TestSessionUnseenReusesTheConnection proves the session routes STATUS through the
// same persistent client rather than opening a second one.
func TestSessionUnseenReusesTheConnection(t *testing.T) {
	connects := 0
	connect := func(config.Account) (sessionClient, error) {
		connects++

		return &fakeClient{unseen: 4}, nil
	}
	session := newSession(config.Account{Name: "fastmail"}, connect)

	for i := 0; i < 2; i++ {
		count, err := session.Unseen("INBOX")
		if err != nil {
			t.Fatalf("Unseen call %d: %v", i, err)
		}
		if count != 4 {
			t.Errorf("Unseen = %d, want 4", count)
		}
	}

	if connects != 1 {
		t.Errorf("connect calls = %d, want 1", connects)
	}
}
