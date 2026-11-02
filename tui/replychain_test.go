package tui

import (
	"strings"
	"testing"

	"github.com/renderorange/chute/mail"
)

// threadChainModel is a reply target three messages deep: it is the parent, its
// envelope names the grandparent, and its cached body carries the full ancestor
// chain, which is what the body fetch returns after #24.
func threadChainModel(t *testing.T) Model {
	t.Helper()

	messages := sampleMessages(1)
	messages[0].MessageID = "<parent@example.org>"
	messages[0].InReplyTo = []string{"<grandparent@example.org>"}

	m, _ := withComposeModelMessages(t, messages)

	// Replace the cached body with the one the server would return, chain included.
	m.bodies[bodyKey{account: m.accountName(), folder: m.openFolder, uid: messages[0].UID}] = &mail.Body{
		Text:       "original text",
		References: []string{"<root@example.org>", "<grandparent@example.org>"},
	}

	return m
}

// unfoldHeader joins a header value that go-message wrapped across lines, so an
// assertion can name the whole chain in one string.
func unfoldHeader(raw string) string {
	raw = strings.ReplaceAll(raw, "\r\n ", " ")
	raw = strings.ReplaceAll(raw, "\n ", " ")

	return raw
}

// TestReplyCarriesTheFullReferencesChain is the defect in #24: a reply to the third
// message of a thread used to carry only the parent, because the envelope holds no
// References. Root, grandparent and parent, in that order.
func TestReplyCarriesTheFullReferencesChain(t *testing.T) {
	m := threadChainModel(t)
	m = press(t, m, keyMsg("r"))

	if m.compose == nil {
		t.Fatal("r opened no composer")
	}

	raw, err := m.messageFor()
	if err != nil {
		t.Fatalf("messageFor: %v", err)
	}

	for _, want := range []string{
		"In-Reply-To: <parent@example.org>",
		"References: <root@example.org> <grandparent@example.org> <parent@example.org>",
	} {
		if !strings.Contains(unfoldHeader(string(raw)), want) {
			t.Errorf("the built reply is missing %q:\n%s", want, raw)
		}
	}
}

// TestReplyAllCarriesTheFullReferencesChain is the same claim on the reply-all path.
func TestReplyAllCarriesTheFullReferencesChain(t *testing.T) {
	m := threadChainModel(t)
	m = press(t, m, keyMsg("R"))

	if m.compose == nil {
		t.Fatal("R opened no composer")
	}

	raw, err := m.messageFor()
	if err != nil {
		t.Fatalf("messageFor: %v", err)
	}

	if want := "References: <root@example.org> <grandparent@example.org> <parent@example.org>"; !strings.Contains(unfoldHeader(string(raw)), want) {
		t.Errorf("the built reply-all is missing %q:\n%s", want, raw)
	}
}

// TestReplyWithoutAChainCarriesOneEntry proves the fallback: a parent with no
// References header of its own still produces In-Reply-To plus a one-entry chain.
func TestReplyWithoutAChainCarriesOneEntry(t *testing.T) {
	messages := sampleMessages(1)
	messages[0].MessageID = "<parent@example.org>"

	m, _ := withComposeModelMessages(t, messages)
	m = press(t, m, keyMsg("r"))

	raw, err := m.messageFor()
	if err != nil {
		t.Fatalf("messageFor: %v", err)
	}

	for _, want := range []string{
		"In-Reply-To: <parent@example.org>",
		"References: <parent@example.org>",
	} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("the built reply is missing %q:\n%s", want, raw)
		}
	}
}

// TestDraftKeepsTheFullReferencesChain proves the chain survives ctrl+s: the draft
// bytes written to the server carry the whole chain, so reopening it cannot lose it.
func TestDraftKeepsTheFullReferencesChain(t *testing.T) {
	messages := sampleMessages(1)
	messages[0].MessageID = "<parent@example.org>"
	messages[0].InReplyTo = []string{"<grandparent@example.org>"}

	m, fakes := withComposeModelMessages(t, messages)
	m.bodies[bodyKey{account: m.accountName(), folder: m.openFolder, uid: messages[0].UID}] = &mail.Body{
		Text:       "original text",
		References: []string{"<root@example.org>", "<grandparent@example.org>"},
	}

	m = press(t, m, keyMsg("r"))
	m = press(t, m, namedKey("ctrl+s"))

	if m.status != "draft saved" {
		t.Errorf("status = %q", m.status)
	}

	if len(fakes.savedMsg) == 0 {
		t.Fatal("no draft bytes reached the server")
	}
	if want := "References: <root@example.org> <grandparent@example.org> <parent@example.org>"; !strings.Contains(unfoldHeader(string(fakes.savedMsg)), want) {
		t.Errorf("the draft is missing %q:\n%s", want, fakes.savedMsg)
	}
}
