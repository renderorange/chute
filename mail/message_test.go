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
