package mail

import (
	"testing"

	"github.com/emersion/go-imap/v2"
)

// TestClientSupportsCapability pins the lookup the delete policy depends on.
// A nil set must report no support rather than panicking or guessing.
func TestClientSupportsCapability(t *testing.T) {
	client := &Client{caps: imap.CapSet{imap.CapMove: {}, imap.CapUIDPlus: {}}}

	if !client.Supports(imap.CapMove) {
		t.Error("MOVE advertised but Supports(CapMove) = false")
	}
	if !client.Supports(imap.CapUIDPlus) {
		t.Error("UIDPLUS advertised but Supports(CapUIDPlus) = false")
	}
	if client.Supports(imap.CapIdle) {
		t.Error("Supports(CapIdle) = true for an unadvertised capability")
	}

	var unset Client
	if unset.Supports(imap.CapMove) {
		t.Error("a client with no capability set reported MOVE support")
	}
}

// TestAllMessagesIsValueSeqSet guards the panic that crashed the UI when a
// folder opened. go-imap's imapwire.NumSetKind type-switches on the value types
// and panics on a *imap.SeqSet.
func TestAllMessagesIsValueSeqSet(t *testing.T) {
	numSet := allMessages()

	if _, ok := numSet.(imap.SeqSet); !ok {
		t.Fatalf("allMessages() = %T, want imap.SeqSet; a pointer panics in NumSetKind", numSet)
	}
	if !numSet.Dynamic() {
		t.Error("allMessages() is not dynamic; it should cover 1:*")
	}
}
