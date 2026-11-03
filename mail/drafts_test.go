package mail

import (
	"errors"
	"strings"
	"testing"

	"github.com/emersion/go-imap/v2"
	"github.com/renderorange/chute/config"
)

// testSession wires a real client to a Session, which is the shape the TUI uses:
// the session owns the connection and the client is the same object.
func testSession(t *testing.T, server *testServer) (*Session, *Client) {
	t.Helper()

	client := server.connect(t)
	session := newSession(server.account(), func(config.Account) (sessionClient, error) {
		return client, nil
	})
	t.Cleanup(func() { session.Close() })

	return session, client
}

// TestSaveDraftAppearsInDrafts proves a saved draft is a real message in the Drafts
// mailbox with the draft flag set, which is what makes it visible in other clients.
func TestSaveDraftAppearsInDrafts(t *testing.T) {
	server := newTestServer(t, testCaps())
	if err := server.user.Create("Drafts", nil); err != nil {
		t.Fatalf("creating Drafts: %v", err)
	}

	session, client := testSession(t, server)

	raw, err := BuildMessage(Compose{
		From:    "blaine@example.org",
		To:      []string{"one@example.com"},
		Subject: "half written",
		Text:    "sed do eius",
	})
	if err != nil {
		t.Fatalf("BuildMessage: %v", err)
	}

	uid, known, err := session.SaveDraft("Drafts", raw)
	if err != nil {
		t.Fatalf("SaveDraft: %v", err)
	}
	if !known || uid == 0 {
		t.Fatalf("SaveDraft returned uid %d, known %v; the server advertises UIDPLUS", uid, known)
	}

	messages, err := client.FetchEnvelopes("Drafts")
	if err != nil {
		t.Fatalf("FetchEnvelopes: %v", err)
	}
	if len(messages) != 1 {
		t.Fatalf("Drafts holds %d messages, want 1", len(messages))
	}
	if !messages[0].HasFlag(imap.FlagDraft) {
		t.Errorf("the saved draft has flags %v, want \\Draft", messages[0].Flags)
	}
}

// TestFetchComposeRoundTripsDraft proves reopening a draft restores the fields the
// composer edits, including the reply headers.
func TestFetchComposeRoundTripsDraft(t *testing.T) {
	server := newTestServer(t, testCaps())
	if err := server.user.Create("Drafts", nil); err != nil {
		t.Fatalf("creating Drafts: %v", err)
	}

	session, client := testSession(t, server)

	original := Compose{
		From:       "Blaine <blaine@example.org>",
		To:         []string{"one@example.com", "Two <two@example.com>"},
		Cc:         []string{"three@example.com"},
		Subject:    "Re: something",
		Text:       "my reply",
		MessageID:  "draft-id@example.org",
		InReplyTo:  "parent@example.org",
		References: []string{"root@example.org", "parent@example.org"},
	}

	raw, err := BuildMessage(original)
	if err != nil {
		t.Fatalf("BuildMessage: %v", err)
	}

	uid, _, err := session.SaveDraft("Drafts", raw)
	if err != nil {
		t.Fatalf("SaveDraft: %v", err)
	}

	got, err := client.FetchCompose("Drafts", uid)
	if err != nil {
		t.Fatalf("FetchCompose: %v", err)
	}

	if got.From != original.From {
		t.Errorf("From = %q, want %q", got.From, original.From)
	}
	if got.Subject != original.Subject {
		t.Errorf("Subject = %q, want %q", got.Subject, original.Subject)
	}
	if strings.TrimSpace(got.Text) != original.Text {
		t.Errorf("Text = %q, want %q", got.Text, original.Text)
	}
	if got.MessageID != original.MessageID {
		t.Errorf("MessageID = %q, want %q", got.MessageID, original.MessageID)
	}
	if got.InReplyTo != original.InReplyTo {
		t.Errorf("InReplyTo = %q, want %q", got.InReplyTo, original.InReplyTo)
	}
	if len(got.To) != 2 || !strings.Contains(got.To[1], "two@example.com") {
		t.Errorf("To = %v, want both recipients", got.To)
	}
	if len(got.Cc) != 1 || !strings.Contains(got.Cc[0], "three@example.com") {
		t.Errorf("Cc = %v", got.Cc)
	}
	if len(got.References) != 2 || got.References[0] != "root@example.org" {
		t.Errorf("References = %v", got.References)
	}
}

// TestFetchComposeWithoutReplyHeaders proves a message with no Message-ID still opens
// as a draft: a reply to such a message must be possible.
func TestFetchComposeWithoutReplyHeaders(t *testing.T) {
	server := newTestServer(t, testCaps())
	client := server.connect(t)
	uid := server.appendMessage(t, "INBOX", "no ids")

	got, err := client.FetchCompose("INBOX", uid)
	if err != nil {
		t.Fatalf("FetchCompose: %v", err)
	}
	if got.MessageID != "" || got.InReplyTo != "" || len(got.References) != 0 {
		t.Errorf("got MessageID %q, InReplyTo %q, References %v; want all empty",
			got.MessageID, got.InReplyTo, got.References)
	}
}

// TestAppendSentUsesOverride proves the sent mailbox is resolved from the config
// override and receives the copy.
func TestAppendSentUsesOverride(t *testing.T) {
	server := newTestServer(t, testCaps())
	if err := server.user.Create("Sent Items", nil); err != nil {
		t.Fatalf("creating Sent: %v", err)
	}

	client := server.connect(t)
	acct := server.account()
	acct.IMAP.Sent = "Sent Items"

	session := newSession(acct, func(config.Account) (sessionClient, error) { return client, nil })
	t.Cleanup(func() { session.Close() })

	if _, err := session.Folders(); err != nil {
		t.Fatalf("Folders: %v", err)
	}

	raw, err := BuildMessage(Compose{From: "blaine@example.org", To: []string{"one@example.com"}, Subject: "sent"})
	if err != nil {
		t.Fatalf("BuildMessage: %v", err)
	}

	if err := session.AppendSent(raw); err != nil {
		t.Fatalf("AppendSent: %v", err)
	}

	messages, err := client.FetchEnvelopes("Sent Items")
	if err != nil {
		t.Fatalf("FetchEnvelopes: %v", err)
	}
	if len(messages) != 1 {
		t.Fatalf("Sent holds %d messages, want 1", len(messages))
	}
}

// TestAppendSentWithoutRoleFolder proves the caller can tell "no sent mailbox" apart
// from a protocol failure, which is what lets the send proceed anyway.
func TestAppendSentWithoutRoleFolder(t *testing.T) {
	server := newTestServer(t, testCaps())

	client := server.connect(t)
	session := newSession(server.account(), func(config.Account) (sessionClient, error) { return client, nil })
	t.Cleanup(func() { session.Close() })

	if _, err := session.Folders(); err != nil {
		t.Fatalf("Folders: %v", err)
	}

	raw, err := BuildMessage(Compose{From: "blaine@example.org", To: []string{"one@example.com"}, Subject: "sent"})
	if err != nil {
		t.Fatalf("BuildMessage: %v", err)
	}

	err = session.AppendSent(raw)
	if err == nil {
		t.Fatal("AppendSent succeeded with no sent mailbox")
	}
	if !errors.Is(err, ErrNoRoleFolder) {
		t.Errorf("error %v does not match ErrNoRoleFolder", err)
	}
}

// TestSaveDraftReportsMissingUID covers the plumbing for a server that returns no
// APPENDUID. It uses the fake because imapmemserver sends APPENDUID even when it does
// not advertise UIDPLUS, so the real server cannot produce this case. When a server
// does send a UID, it is trusted regardless of the capability list: what matters is
// whether the caller can name the message later.
func TestSaveDraftReportsMissingUID(t *testing.T) {
	client := &fakeClient{}
	session := newSession(config.Account{Name: "memory"}, func(config.Account) (sessionClient, error) {
		return client, nil
	})

	raw, err := BuildMessage(Compose{From: "blaine@example.org", To: []string{"one@example.com"}, Subject: "draft"})
	if err != nil {
		t.Fatalf("BuildMessage: %v", err)
	}

	uid, known, err := session.SaveDraft("Drafts", raw)
	if err != nil {
		t.Fatalf("SaveDraft: %v", err)
	}
	if known {
		t.Errorf("SaveDraft reported a known UID (%d) from a client that returned none", uid)
	}
}

// TestSessionFetchCompose covers the wrapper the TUI calls, not just the client method.
func TestSessionFetchCompose(t *testing.T) {
	server := newTestServer(t, testCaps())
	if err := server.user.Create("Drafts", nil); err != nil {
		t.Fatalf("creating Drafts: %v", err)
	}

	session, _ := testSession(t, server)

	raw, err := BuildMessage(Compose{
		From:    "blaine@example.org",
		To:      []string{"one@example.com"},
		Subject: "draft subject",
		Text:    "draft text",
	})
	if err != nil {
		t.Fatalf("BuildMessage: %v", err)
	}

	uid, _, err := session.SaveDraft("Drafts", raw)
	if err != nil {
		t.Fatalf("SaveDraft: %v", err)
	}

	got, err := session.FetchCompose("Drafts", uid)
	if err != nil {
		t.Fatalf("Session.FetchCompose: %v", err)
	}
	if got.Subject != "draft subject" {
		t.Errorf("Subject = %q", got.Subject)
	}
}

// TestDraftsFolderUsesOverride proves the drafts role resolves from the config
// override, which is the path the composer uses to decide whether a message opens in
// the composer or the reader.
func TestDraftsFolderUsesOverride(t *testing.T) {
	server := newTestServer(t, testCaps())
	if err := server.user.Create("Drafts", nil); err != nil {
		t.Fatalf("creating Drafts: %v", err)
	}

	client := server.connect(t)
	acct := server.account()
	acct.IMAP.Drafts = "Drafts"

	session := newSession(acct, func(config.Account) (sessionClient, error) { return client, nil })
	t.Cleanup(func() { session.Close() })

	if _, err := session.Folders(); err != nil {
		t.Fatalf("Folders: %v", err)
	}

	folder, ok := session.DraftsFolder()
	if !ok || folder != "Drafts" {
		t.Errorf("DraftsFolder() = %q, %v; want Drafts, true", folder, ok)
	}
}

// TestDraftsFolderWithoutRole proves the composer is told when no drafts mailbox
// resolves, so saving can report it instead of guessing a name.
func TestDraftsFolderWithoutRole(t *testing.T) {
	server := newTestServer(t, testCaps())
	client := server.connect(t)

	session := newSession(server.account(), func(config.Account) (sessionClient, error) { return client, nil })
	t.Cleanup(func() { session.Close() })

	if _, err := session.Folders(); err != nil {
		t.Fatalf("Folders: %v", err)
	}

	if folder, ok := session.DraftsFolder(); ok {
		t.Errorf("DraftsFolder() = %q, true; want no drafts mailbox", folder)
	}
}
