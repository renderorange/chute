package mail

import (
	"strings"
	"testing"
	"time"
)

// TestBuildMessageWritesBccForDelivery is the delivery half of #62: the Bcc header must
// be in the bytes handed to SMTP, because envelopeOf derives RCPT TO from the headers.
func TestBuildMessageWritesBccForDelivery(t *testing.T) {
	raw, err := BuildMessage(Compose{
		From:    "me@example.org",
		To:      []string{"one@example.com"},
		Cc:      []string{"two@example.com"},
		Bcc:     []string{"blind@example.com"},
		Subject: "an introduction",
		Text:    "hello",
	})
	if err != nil {
		t.Fatalf("BuildMessage: %v", err)
	}

	if got, want := headerValue(t, raw, "Bcc"), "blind@example.com"; got != want {
		t.Errorf("Bcc = %q, want %q", got, want)
	}

	env, err := envelopeOf(raw)
	if err != nil {
		t.Fatalf("envelopeOf: %v", err)
	}
	if got := strings.Join(env.rcpt, " "); got != "one@example.com two@example.com blind@example.com" {
		t.Errorf("RCPT TO = %q, want To, Cc and Bcc", got)
	}
}

// TestBuildMessageOmitsEmptyBcc keeps the header out entirely when there is nothing to
// put in it; an empty Bcc both looks wrong and is a header the user did not write.
func TestBuildMessageOmitsEmptyBcc(t *testing.T) {
	for _, bcc := range [][]string{nil, {""}, {"  ", ""}} {
		raw, err := BuildMessage(Compose{
			From:    "me@example.org",
			To:      []string{"one@example.com"},
			Bcc:     bcc,
			Subject: "no blind copy",
		})
		if err != nil {
			t.Fatalf("BuildMessage(%q): %v", bcc, err)
		}

		if header := headerBlock(raw); strings.Contains(header, "Bcc:") {
			t.Errorf("Bcc %q produced a Bcc header:\n%s", bcc, header)
		}
	}
}

// TestBuildMessageRejectsNewlineInBcc is the header-injection gate, the same rule To and
// Cc already follow.
func TestBuildMessageRejectsNewlineInBcc(t *testing.T) {
	_, err := BuildMessage(Compose{
		From:    "me@example.org",
		To:      []string{"one@example.com"},
		Bcc:     []string{"blind@example.com\r\nBcc: evil@example.com"},
		Subject: "injection",
	})
	if err == nil || !strings.Contains(err.Error(), "Bcc") {
		t.Fatalf("error = %v, want a Bcc header-injection refusal", err)
	}
}

// TestSendMessageDeliversToBlindRecipients drives the real SMTP conversation: the blind
// address has to reach RCPT TO, and the transmitted DATA keeps the header, which is what
// makes the delivery blind rather than undisclosed-but-missing.
func TestSendMessageDeliversToBlindRecipients(t *testing.T) {
	server := newScriptedServer(t, true, false, false)

	raw, err := BuildMessage(Compose{
		From:    "me@example.org",
		To:      []string{"one@example.com"},
		Bcc:     []string{"blind@example.com"},
		Subject: "blind",
		Text:    "hello",
	})
	if err != nil {
		t.Fatalf("BuildMessage: %v", err)
	}

	if err := sendMessage(testAccount(t, server.addr, true), "smtp-secret", raw, dialScripted, 5*time.Second); err != nil {
		t.Fatalf("sendMessage: %v", err)
	}

	<-server.served()

	joined := strings.Join(server.commandsCopy(), "\n")
	for _, want := range []string{"RCPT TO:<one@example.com>", "RCPT TO:<blind@example.com>"} {
		if !strings.Contains(joined, want) {
			t.Errorf("the session does not contain %q:\n%s", want, joined)
		}
	}
	if !strings.Contains(normalizeCRLF(server.dataCopy()), "Bcc: blind@example.com") {
		t.Errorf("the transmitted message lost the blind header:\n%s", server.dataCopy())
	}
}

// TestStripBccLeavesTheRestOfTheMessageAlone is the blind half: the copy filed in Sent
// must not name the blind recipients, and must otherwise be the same message.
func TestStripBccLeavesTheRestOfTheMessageAlone(t *testing.T) {
	raw, err := BuildMessage(Compose{
		From:    "me@example.org",
		To:      []string{"one@example.com"},
		Cc:      []string{"two@example.com"},
		Bcc:     []string{"blind@example.com"},
		Subject: "an introduction",
		Text:    "Bcc: this is body text, not a header\nsecond line",
	})
	if err != nil {
		t.Fatalf("BuildMessage: %v", err)
	}

	filed := StripBcc(raw)

	header := headerBlock(filed)
	if strings.Contains(header, "Bcc:") {
		t.Errorf("the filed copy still names the blind recipients:\n%s", header)
	}
	for _, want := range []string{"To: one@example.com", "Cc: two@example.com", "Subject: an introduction"} {
		if !strings.Contains(unfold(filed), want) {
			t.Errorf("the filed copy is missing %q:\n%s", want, header)
		}
	}

	// The body is untouched, including a line that only looks like a header.
	if !strings.Contains(string(filed), "Bcc: this is body text, not a header") {
		t.Errorf("StripBcc edited the body:\n%s", filed)
	}
	if body(raw) != body(filed) {
		t.Errorf("StripBcc changed the body bytes:\ngot  %q\nwant %q", body(filed), body(raw))
	}
}

// TestStripBccRemovesFoldedValue proves a long blind list, which the writer folds across
// lines, loses its continuation lines too instead of leaving them as broken headers.
func TestStripBccRemovesFoldedValue(t *testing.T) {
	raw := []byte("From: me@example.org\r\n" +
		"To: one@example.com\r\n" +
		"Bcc: blind1@example.com,\r\n" +
		" blind2@example.com\r\n" +
		"Subject: folded\r\n" +
		"\r\n" +
		"body\r\n")

	filed := string(StripBcc(raw))

	if strings.Contains(filed, "blind1@example.com") || strings.Contains(filed, "blind2@example.com") {
		t.Errorf("StripBcc left a folded Bcc behind:\n%s", filed)
	}
	if !strings.Contains(filed, "Subject: folded") || !strings.HasSuffix(filed, "\r\n\r\nbody\r\n") {
		t.Errorf("StripBcc damaged the surviving headers or the body:\n%q", filed)
	}
}

// TestParseComposeKeepsBccForADraft proves the blind recipients survive a restart: the
// draft is the only place they can live if a reopened draft is to send them again.
func TestParseComposeKeepsBccForADraft(t *testing.T) {
	raw, err := BuildMessage(Compose{
		From:    "me@example.org",
		To:      []string{"one@example.com"},
		Bcc:     []string{"Blind Person <blind@example.com>", "other@example.com"},
		Subject: "draft",
		Text:    "half written",
	})
	if err != nil {
		t.Fatalf("BuildMessage: %v", err)
	}

	draft, err := parseCompose(raw)
	if err != nil {
		t.Fatalf("parseCompose: %v", err)
	}

	want := "\"Blind Person\" <blind@example.com> <other@example.com>"
	if got := strings.Join(draft.Bcc, " "); got != want {
		t.Errorf("Bcc = %q, want %q", got, want)
	}
}

// TestReplyAndForwardDoNotInheritBcc keeps a blind recipient from being re-addressed by
// accident: replying to a message that carries Bcc (a reopened draft, say) must not copy
// them.
func TestReplyAndForwardDoNotInheritBcc(t *testing.T) {
	orig := Compose{
		From:    "sender@example.org",
		To:      []string{"me@example.org"},
		Bcc:     []string{"blind@example.org"},
		Subject: "the subject",
		Text:    "the text",
	}

	for name, got := range map[string]Compose{
		"reply":        ReplyCompose(orig, ReplyOptions{From: "me@example.org", All: true}),
		"forward":      ForwardCompose(orig, "me@example.org", ""),
		"reply-bottom": ReplyCompose(orig, ReplyOptions{From: "me@example.org", All: true, Bottom: true}),
	} {
		if len(got.Bcc) != 0 {
			t.Errorf("%s carried Bcc %v", name, got.Bcc)
		}
	}
}

// headerBlock returns the header section of a built message.
func headerBlock(raw []byte) string {
	text := string(raw)
	if i := strings.Index(text, "\r\n\r\n"); i >= 0 {
		return text[:i]
	}

	return text
}

// body returns everything after the header separator.
func body(raw []byte) string {
	text := string(raw)
	if i := strings.Index(text, "\r\n\r\n"); i >= 0 {
		return text[i+4:]
	}

	return ""
}

// unfold removes header folding so a value can be compared as one line.
func unfold(raw []byte) string {
	return strings.ReplaceAll(string(raw), "\r\n ", " ")
}

// headerValue returns the unfolded value of one header, or "" when it is absent.
func headerValue(t *testing.T, raw []byte, name string) string {
	t.Helper()

	for _, line := range strings.Split(unfold(raw), "\r\n") {
		key, value, ok := strings.Cut(line, ":")
		if ok && strings.EqualFold(strings.TrimSpace(key), name) {
			return strings.TrimSpace(value)
		}
	}

	return ""
}
