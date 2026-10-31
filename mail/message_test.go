package mail

import (
	"testing"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
)

func TestMessageHelpers(t *testing.T) {
	m := Message{
		UID:   7,
		Flags: []imap.Flag{imap.FlagSeen},
		Envelope: imap.Envelope{
			Subject: "Quarterly numbers",
			From:    []imap.Address{{Name: "Ada Lovelace", Mailbox: "ada", Host: "example.org"}},
			Date:    time.Date(2026, 10, 29, 9, 0, 0, 0, time.UTC),
		},
	}

	if !m.Seen() {
		t.Error("Seen = false, want true")
	}
	if m.Flagged() {
		t.Error("Flagged = true, want false")
	}
	if got, want := m.Subject(), "Quarterly numbers"; got != want {
		t.Errorf("Subject = %q, want %q", got, want)
	}
	if got, want := m.From(), "Ada Lovelace"; got != want {
		t.Errorf("From = %q, want %q", got, want)
	}
	if got, want := m.Date().Year(), 2026; got != want {
		t.Errorf("Date year = %d, want %d", got, want)
	}
}

// TestMessageSetFlag proves the local mirror of a confirmed server change adds,
// removes and stays idempotent.
func TestMessageSetFlag(t *testing.T) {
	m := Message{UID: 1}

	m.SetFlag(imap.FlagSeen, true)
	if !m.Seen() {
		t.Fatal("SetFlag(+Seen) did not add the flag")
	}

	m.SetFlag(imap.FlagSeen, true)
	if got := len(m.Flags); got != 1 {
		t.Errorf("flags = %v, want one entry after a repeated add", m.Flags)
	}

	m.SetFlag(imap.FlagSeen, false)
	if m.Seen() {
		t.Error("SetFlag(-Seen) did not remove the flag")
	}

	m.SetFlag(imap.FlagSeen, false)
	if got := len(m.Flags); got != 0 {
		t.Errorf("flags = %v, want empty after a repeated remove", m.Flags)
	}

	m.SetFlag(imap.FlagFlagged, true)
	if !m.Flagged() {
		t.Error("SetFlag(+Flagged) did not add the flag")
	}
}

func TestMessageDefaults(t *testing.T) {
	envelope := imap.Envelope{From: []imap.Address{{Mailbox: "noreply", Host: "example.org"}}}
	internal := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	m := Message{Envelope: envelope, InternalDate: internal}

	if got, want := m.Subject(), "(no subject)"; got != want {
		t.Errorf("Subject = %q, want %q", got, want)
	}
	if got, want := m.From(), "noreply@example.org"; got != want {
		t.Errorf("From = %q, want the address when no name is set", got)
	}
	if got := m.Date(); !got.Equal(internal) {
		t.Errorf("Date = %v, want the internal date %v", got, internal)
	}

	empty := Message{}
	if got, want := empty.From(), ""; got != want {
		t.Errorf("From = %q, want empty", got)
	}
}

func TestMessagesFromBuffers(t *testing.T) {
	buffers := []*imapclient.FetchMessageBuffer{
		{
			SeqNum:       1,
			UID:          42,
			Flags:        []imap.Flag{imap.FlagSeen, imap.FlagFlagged},
			RFC822Size:   2048,
			InternalDate: time.Date(2026, 10, 2, 0, 0, 0, 0, time.UTC),
			Envelope:     &imap.Envelope{Subject: "hello"},
		},
		{
			SeqNum:   2,
			UID:      43,
			Envelope: nil,
		},
	}

	messages := messagesFromBuffers(buffers)

	if len(messages) != 2 {
		t.Fatalf("messages = %d, want 2", len(messages))
	}
	if got, want := messages[0].UID, imap.UID(42); got != want {
		t.Errorf("UID = %d, want %d", got, want)
	}
	if !messages[0].Flagged() {
		t.Error("first message should be flagged")
	}
	if got, want := messages[0].Size, int64(2048); got != want {
		t.Errorf("Size = %d, want %d", got, want)
	}
	if got, want := messages[0].Subject(), "hello"; got != want {
		t.Errorf("Subject = %q, want %q", got, want)
	}
	// A nil envelope must not panic and must fall back cleanly.
	if got, want := messages[1].Subject(), "(no subject)"; got != want {
		t.Errorf("nil-envelope Subject = %q, want %q", got, want)
	}
}

// TestMessagesFromBuffersThreadingHeaders pins that the two envelope fields
// threading reads survive the mapping, and that a root message keeps them empty.
func TestMessagesFromBuffersThreadingHeaders(t *testing.T) {
	buffers := []*imapclient.FetchMessageBuffer{
		{
			UID: 10,
			Envelope: &imap.Envelope{
				Subject:   "reply",
				MessageID: "<reply@example.org>",
				InReplyTo: []string{"<parent@example.org>"},
			},
		},
		{
			UID:      11,
			Envelope: &imap.Envelope{Subject: "root"},
		},
	}

	messages := messagesFromBuffers(buffers)

	if got, want := messages[0].MessageID, "<reply@example.org>"; got != want {
		t.Errorf("MessageID = %q, want %q", got, want)
	}
	if len(messages[0].InReplyTo) != 1 || messages[0].InReplyTo[0] != "<parent@example.org>" {
		t.Errorf("InReplyTo = %v, want the parent id", messages[0].InReplyTo)
	}
	if got := messages[1].MessageID; got != "" {
		t.Errorf("MessageID for a root = %q, want empty", got)
	}
	if got := messages[1].InReplyTo; len(got) != 0 {
		t.Errorf("InReplyTo for a root = %v, want empty", got)
	}
}

// TestMessageAddressAccessors proves the reply builders get full addresses while the
// list column keeps the display name.
func TestMessageAddressAccessors(t *testing.T) {
	msg := Message{Envelope: imap.Envelope{
		From: []imap.Address{{Name: "Sender", Mailbox: "sender", Host: "example.org"}},
		To:   []imap.Address{{Name: "Other", Mailbox: "other", Host: "example.org"}, {Mailbox: "bare", Host: "example.org"}},
		Cc:   []imap.Address{{Name: "Carbon", Mailbox: "carbon", Host: "example.org"}},
	}}

	if got, want := msg.From(), "Sender"; got != want {
		t.Errorf("From() = %q, want the display name %q", got, want)
	}
	if got, want := msg.FromAddress(), "Sender <sender@example.org>"; got != want {
		t.Errorf("FromAddress() = %q, want %q", got, want)
	}

	to := msg.ToAddresses()
	if len(to) != 2 || to[0] != "Other <other@example.org>" || to[1] != "bare@example.org" {
		t.Errorf("ToAddresses() = %v", to)
	}

	cc := msg.CcAddresses()
	if len(cc) != 1 || cc[0] != "Carbon <carbon@example.org>" {
		t.Errorf("CcAddresses() = %v", cc)
	}
}
