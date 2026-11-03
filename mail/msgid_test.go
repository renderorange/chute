package mail

import (
	"strings"
	"testing"
)

// TestBuildMessageBracketsBareThreadingIDs is the #71 defect at the boundary: the
// envelope gives bare ids, so a reply built from them must gain exactly one pair of
// angle brackets per id. Bare ids are the canonical internal form; this is where they
// become RFC 5322 msg-ids.
func TestBuildMessageBracketsBareThreadingIDs(t *testing.T) {
	raw, err := BuildMessage(Compose{
		From:       "me@example.org",
		To:         []string{"you@example.org"},
		Subject:    "Re: the thread",
		MessageID:  "reply@example.org",
		InReplyTo:  "parent@example.org",
		References: []string{"root@example.org", "parent@example.org"},
	})
	if err != nil {
		t.Fatalf("BuildMessage: %v", err)
	}

	entity := readCompose(t, raw)

	if got, want := entity.Header.Get("Message-ID"), "<reply@example.org>"; got != want {
		t.Errorf("Message-ID = %q, want %q", got, want)
	}
	if got, want := entity.Header.Get("In-Reply-To"), "<parent@example.org>"; got != want {
		t.Errorf("In-Reply-To = %q, want %q", got, want)
	}
	if got, want := entity.Header.Get("References"), "<root@example.org> <parent@example.org>"; got != want {
		t.Errorf("References = %q, want %q", got, want)
	}
}

// TestBuildMessageDoesNotDoubleBracket proves an already-bracketed id is normalized
// rather than wrapped twice, so an old draft or a hand-built Compose cannot produce
// <<id>>.
func TestBuildMessageDoesNotDoubleBracket(t *testing.T) {
	raw, err := BuildMessage(Compose{
		From:       "me@example.org",
		To:         []string{"you@example.org"},
		Subject:    "Re: the thread",
		MessageID:  "<reply@example.org>",
		InReplyTo:  "<parent@example.org>",
		References: []string{"<root@example.org>"},
	})
	if err != nil {
		t.Fatalf("BuildMessage: %v", err)
	}

	entity := readCompose(t, raw)

	for name, want := range map[string]string{
		"Message-ID":  "<reply@example.org>",
		"In-Reply-To": "<parent@example.org>",
		"References":  "<root@example.org>",
	} {
		if got := entity.Header.Get(name); got != want {
			t.Errorf("%s = %q, want %q", name, got, want)
		}
	}
}

// TestReplyComposeNormalizesEnvelopeIDs pins the shape at the seam the bug lived in:
// the original's id comes from the envelope without brackets, while its ancestor chain
// comes from a header with them. ReplyCompose must hold one form, and BuildMessage must
// render the other.
func TestReplyComposeNormalizesEnvelopeIDs(t *testing.T) {
	got := ReplyCompose(Compose{
		From:       "sender@example.org",
		To:         []string{"me@example.org"},
		Subject:    "the thread",
		Text:       "original",
		MessageID:  "parent@example.org",
		References: []string{"<root@example.org>", "<grandparent@example.org>"},
	}, ReplyOptions{From: "me@example.org"})

	if got.InReplyTo != "parent@example.org" {
		t.Errorf("InReplyTo = %q, want the bare parent id", got.InReplyTo)
	}
	if want := []string{"root@example.org", "grandparent@example.org", "parent@example.org"}; strings.Join(got.References, " ") != strings.Join(want, " ") {
		t.Errorf("References = %v, want %v", got.References, want)
	}

	raw, err := BuildMessage(got)
	if err != nil {
		t.Fatalf("BuildMessage: %v", err)
	}

	entity := readCompose(t, raw)
	if want := "<root@example.org> <grandparent@example.org> <parent@example.org>"; entity.Header.Get("References") != want {
		t.Errorf("References header = %q, want %q", entity.Header.Get("References"), want)
	}
	if want := "<parent@example.org>"; entity.Header.Get("In-Reply-To") != want {
		t.Errorf("In-Reply-To header = %q, want %q", entity.Header.Get("In-Reply-To"), want)
	}
}

// TestParseBodyStripsBrackets proves the body fetch, which is the source of the
// ancestor chain, produces the same canonical bare form the envelope uses.
func TestParseBodyStripsBrackets(t *testing.T) {
	body, err := parseBody([]byte("From: sender@example.com\r\n" +
		"Subject: re: the thread\r\n" +
		"References: <root@example.com> <grandparent@example.com>\r\n" +
		"\r\n" +
		"the text\r\n"))
	if err != nil {
		t.Fatalf("parseBody: %v", err)
	}

	if want := []string{"root@example.com", "grandparent@example.com"}; strings.Join(body.References, " ") != strings.Join(want, " ") {
		t.Errorf("References = %v, want %v", body.References, want)
	}
}
