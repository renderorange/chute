package mail

import (
	"testing"

	"github.com/emersion/go-imap/v2"
)

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
