package mail

import (
	"bytes"
	"regexp"
	"strings"
	"testing"

	"github.com/emersion/go-message"

	"io"
)

// readCompose parses a built message back through go-message, so the assertions are
// about what a receiving client sees rather than about the bytes we happened to
// write.
func readCompose(t *testing.T, raw []byte) *message.Entity {
	t.Helper()

	entity, err := message.Read(bytes.NewReader(raw))
	if err != nil && !message.IsUnknownCharset(err) && !message.IsUnknownEncoding(err) {
		t.Fatalf("reading the built message: %v", err)
	}
	if entity == nil {
		t.Fatal("reading the built message: no content")
	}

	return entity
}

// TestBuildMessageHeaders covers the fields every outgoing message carries.
func TestBuildMessageHeaders(t *testing.T) {
	raw, err := BuildMessage(Compose{
		From:    "Blaine <blaine@example.org>",
		To:      []string{"one@example.com", "Two <two@example.com>"},
		Subject: "hello",
		Text:    "body text",
	})
	if err != nil {
		t.Fatalf("BuildMessage: %v", err)
	}

	entity := readCompose(t, raw)

	if got := entity.Header.Get("From"); got != "Blaine <blaine@example.org>" {
		t.Errorf("From = %q", got)
	}
	if got := entity.Header.Get("Subject"); got != "hello" {
		t.Errorf("Subject = %q", got)
	}
	for _, want := range []string{"MIME-Version", "Date", "Message-ID", "Content-Type"} {
		if !entity.Header.Has(want) {
			t.Errorf("the message has no %s header", want)
		}
	}
	if got := entity.Header.Get("Content-Type"); !strings.HasPrefix(got, "text/plain") {
		t.Errorf("Content-Type = %q, want text/plain", got)
	}
	if entity.Header.Has("Cc") {
		t.Error("an empty Cc was written as a header")
	}

	body, err := io.ReadAll(entity.Body)
	if err != nil {
		t.Fatalf("reading the body: %v", err)
	}
	if got := string(body); got != "body text" {
		t.Errorf("body = %q, want the text unchanged", got)
	}
}

// TestBuildMessageCRLF proves every line break is CRLF, which SMTP requires and which
// a Go string literal of "\n" would otherwise get wrong.
func TestBuildMessageCRLF(t *testing.T) {
	raw, err := BuildMessage(Compose{
		From:    "blaine@example.org",
		To:      []string{"one@example.com"},
		Subject: "lines",
		Text:    "first\nsecond\r\nthird",
	})
	if err != nil {
		t.Fatalf("BuildMessage: %v", err)
	}

	for i, b := range raw {
		if b == '\n' && (i == 0 || raw[i-1] != '\r') {
			t.Fatalf("bare newline at byte %d: %q", i, raw)
		}
	}
}

// TestBuildMessageUsesTheGivenMessageID pins the one-id-per-message rule: the draft
// append and the SMTP send must carry the same Message-ID.
func TestBuildMessageUsesTheGivenMessageID(t *testing.T) {
	const id = "<fixed-id@example.org>"

	raw, err := BuildMessage(Compose{
		From:      "blaine@example.org",
		To:        []string{"one@example.com"},
		Subject:   "id",
		MessageID: id,
	})
	if err != nil {
		t.Fatalf("BuildMessage: %v", err)
	}

	if got := readCompose(t, raw).Header.Get("Message-ID"); got != id {
		t.Errorf("Message-ID = %q, want %q", got, id)
	}
}

// TestBuildMessageGeneratesMessageID covers the generated case and the domain fallback.
func TestBuildMessageGeneratesMessageID(t *testing.T) {
	shape := regexp.MustCompile(`^<[^<>@\s]+@[^<>@\s]+>$`)

	first, err := BuildMessage(Compose{From: "blaine@example.org", To: []string{"one@example.com"}, Subject: "a"})
	if err != nil {
		t.Fatalf("BuildMessage: %v", err)
	}
	second, err := BuildMessage(Compose{From: "blaine@example.org", To: []string{"one@example.com"}, Subject: "b"})
	if err != nil {
		t.Fatalf("BuildMessage: %v", err)
	}

	firstID := readCompose(t, first).Header.Get("Message-ID")
	secondID := readCompose(t, second).Header.Get("Message-ID")

	if !shape.MatchString(firstID) {
		t.Errorf("generated Message-ID %q does not match %s", firstID, shape)
	}
	if !strings.HasSuffix(firstID, "@example.org>") {
		t.Errorf("generated Message-ID %q does not use the From domain", firstID)
	}
	if firstID == secondID {
		t.Error("two separately built messages share a Message-ID")
	}

	noDomain, err := BuildMessage(Compose{From: "blaine", To: []string{"one@example.com"}, Subject: "c"})
	if err != nil {
		t.Fatalf("BuildMessage: %v", err)
	}
	if id := readCompose(t, noDomain).Header.Get("Message-ID"); !strings.HasSuffix(id, "@chute.local>") {
		t.Errorf("Message-ID = %q, want the chute.local fallback", id)
	}
}

// TestBuildMessageReplyHeaders covers the threading headers on a reply.
func TestBuildMessageReplyHeaders(t *testing.T) {
	raw, err := BuildMessage(Compose{
		From:      "blaine@example.org",
		To:        []string{"one@example.com"},
		Subject:   "Re: hello",
		InReplyTo: "<parent@example.org>",
		References: []string{
			"<root@example.org>",
			"<parent@example.org>",
		},
	})
	if err != nil {
		t.Fatalf("BuildMessage: %v", err)
	}

	entity := readCompose(t, raw)
	if got := entity.Header.Get("In-Reply-To"); got != "<parent@example.org>" {
		t.Errorf("In-Reply-To = %q", got)
	}
	refs := entity.Header.Get("References")
	if !strings.Contains(refs, "<root@example.org>") || !strings.Contains(refs, "<parent@example.org>") {
		t.Errorf("References = %q", refs)
	}
}

// TestComposeRoundTripKeepsThreadingHeaders proves the draft path is lossless for the
// headers that thread a reply: BuildMessage writes them into the appended draft and
// parseCompose reads them back when the draft is reopened, so a drafted reply still lands
// under the original when it is finally sent.
func TestComposeRoundTripKeepsThreadingHeaders(t *testing.T) {
	want := Compose{
		From:       "blaine@example.org",
		To:         []string{"one@example.com"},
		Subject:    "Re: hello",
		Text:       "body text",
		MessageID:  "reply@example.org",
		InReplyTo:  "parent@example.org",
		References: []string{"root@example.org", "parent@example.org"},
	}

	raw, err := BuildMessage(want)
	if err != nil {
		t.Fatalf("BuildMessage: %v", err)
	}

	got, err := parseCompose(raw)
	if err != nil {
		t.Fatalf("parseCompose: %v", err)
	}

	if got.MessageID != want.MessageID {
		t.Errorf("Message-ID = %q, want %q", got.MessageID, want.MessageID)
	}
	if got.InReplyTo != want.InReplyTo {
		t.Errorf("In-Reply-To = %q, want %q", got.InReplyTo, want.InReplyTo)
	}
	if strings.Join(got.References, " ") != strings.Join(want.References, " ") {
		t.Errorf("References = %v, want %v", got.References, want.References)
	}
}

// TestBuildMessageKeepsUTF8 proves a non-ASCII subject and body survive the round trip.
func TestBuildMessageKeepsUTF8(t *testing.T) {
	raw, err := BuildMessage(Compose{
		From:    "blaine@example.org",
		To:      []string{"one@example.com"},
		Subject: "Grüße aus Köln",
		Text:    "héllo wörld",
	})
	if err != nil {
		t.Fatalf("BuildMessage: %v", err)
	}

	entity := readCompose(t, raw)
	if got := entity.Header.Get("Subject"); got != "Grüße aus Köln" {
		t.Errorf("Subject = %q", got)
	}

	body, err := io.ReadAll(entity.Body)
	if err != nil {
		t.Fatalf("reading the body: %v", err)
	}
	if !strings.Contains(string(body), "héllo wörld") {
		t.Errorf("body = %q", body)
	}
}

// TestBuildMessageRejectsHeaderInjection proves a newline in a header field is an
// error, not a second header. This is the difference between a validation rule and an
// open relay for Bcc lines.
func TestBuildMessageRejectsHeaderInjection(t *testing.T) {
	tests := []struct {
		name string
		c    Compose
		want string
	}{
		{
			name: "to",
			c:    Compose{From: "blaine@example.org", To: []string{"one@example.com\r\nBcc: evil@example.com"}, Subject: "x"},
			want: "To",
		},
		{
			name: "subject",
			c:    Compose{From: "blaine@example.org", To: []string{"one@example.com"}, Subject: "x\r\nBcc: evil@example.com"},
			want: "Subject",
		},
		{
			name: "from",
			c:    Compose{From: "blaine@example.org\r\nBcc: evil@example.com", To: []string{"one@example.com"}, Subject: "x"},
			want: "From",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			raw, err := BuildMessage(tc.c)
			if err == nil {
				t.Fatalf("BuildMessage accepted a newline in a header and built %q", raw)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %q does not name the field %q", err, tc.want)
			}
			if bytes.Contains(raw, []byte("Bcc:")) {
				t.Errorf("the injected header reached the output: %q", raw)
			}
		})
	}
}
