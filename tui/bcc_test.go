package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/emersion/go-imap/v2"
	"github.com/renderorange/chute/config"
	"github.com/renderorange/chute/mail"
)

// bccHeader returns the header section of a built message, unfolded.
func bccHeader(msg []byte) string {
	text := unfoldHeader(string(msg))
	if i := strings.Index(text, "\r\n\r\n"); i >= 0 {
		return text[:i]
	}

	return text
}

// TestSendKeepsBccForDeliveryAndDropsItFromTheSentCopy is #62 end to end at the model: the
// bytes SMTP transmits name the blind recipient, and the copy filed in Sent does not.
func TestSendKeepsBccForDeliveryAndDropsItFromTheSentCopy(t *testing.T) {
	m, fakes := withComposeModel(t)
	m.compose = newComposer(mail.Compose{
		To:      []string{"one@example.com"},
		Cc:      []string{"two@example.com"},
		Bcc:     []string{"blind@example.com"},
		Subject: "an introduction",
		Text:    "hello",
	}, false)

	m = deliver(t, m, m.sendComposeCmd())

	if len(fakes.sentMsg) == 0 || len(fakes.appendedMsg) == 0 {
		t.Fatalf("send=%d bytes, append=%d bytes", len(fakes.sentMsg), len(fakes.appendedMsg))
	}
	if !strings.Contains(bccHeader(fakes.sentMsg), "Bcc: blind@example.com") {
		t.Errorf("the transmitted message lost the blind recipient:\n%s", bccHeader(fakes.sentMsg))
	}
	if got := bccHeader(fakes.appendedMsg); strings.Contains(got, "Bcc:") {
		t.Errorf("the Sent copy names the blind recipients:\n%s", got)
	}
	for _, want := range []string{"To: one@example.com", "Cc: two@example.com", "Subject: an introduction"} {
		if !strings.Contains(bccHeader(fakes.appendedMsg), want) {
			t.Errorf("the Sent copy is missing %q:\n%s", want, bccHeader(fakes.appendedMsg))
		}
	}
	if !strings.Contains(unfoldHeader(string(fakes.appendedMsg)), "hello") {
		t.Error("the Sent copy lost the body")
	}
}

// TestDraftKeepsBccAndReopeningRestoresIt proves the blind recipients round-trip through
// the Drafts mailbox, which is the only place they can survive a restart.
func TestDraftKeepsBccAndReopeningRestoresIt(t *testing.T) {
	m, fakes := withComposeModel(t)
	m.compose = newComposer(mail.Compose{
		To:      []string{"one@example.com"},
		Bcc:     []string{"blind@example.com"},
		Subject: "half written",
		Text:    "the start",
	}, false)

	m = deliver(t, m, m.saveDraftCmd())

	if got := bccHeader(fakes.savedMsg); !strings.Contains(got, "Bcc: blind@example.com") {
		t.Fatalf("the draft lost the blind recipient:\n%s", got)
	}

	// A fresh process reopens it: the composer must come back with the field filled.
	m.compose = nil
	m.openFolder = "Drafts"
	m.focus = focusMessages
	m.fetchCompose = func(config.Account, string, imap.UID) (*mail.Compose, error) {
		return &mail.Compose{
			From:    "me@example.org",
			To:      []string{"one@example.com"},
			Bcc:     []string{"blind@example.com"},
			Subject: "half written",
			Text:    "the start",
		}, nil
	}

	m = press(t, m, tea.KeyMsg{Type: tea.KeyEnter})

	if m.compose == nil {
		t.Fatal("opening the draft did not open the composer")
	}
	if got := m.compose.bcc.text(); got != "blind@example.com" {
		t.Errorf("Bcc field = %q, want the saved blind recipient", got)
	}
	if got := m.compose.compose().Bcc; len(got) != 1 || got[0] != "blind@example.com" {
		t.Errorf("Bcc = %v, want the saved blind recipient", got)
	}
}
