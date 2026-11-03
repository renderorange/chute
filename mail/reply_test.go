package mail

import (
	"strings"
	"testing"
	"time"
)

// fixedTime is the moment the attribution tests format.
var fixedTime = time.Date(2026, time.October, 30, 14, 2, 0, 0, time.Local)

// TestAttribution covers the line that introduces a quoted original, with and without
// a display name.
func TestAttribution(t *testing.T) {
	tests := []struct {
		name string
		from string
		want string
	}{
		{
			name: "display name",
			from: "Sender Name <sender@example.org>",
			want: "On 2026-10-30 14:02, Sender Name <sender@example.org> wrote:",
		},
		{
			name: "bare address",
			from: "sender@example.org",
			want: "On 2026-10-30 14:02, sender@example.org wrote:",
		},
		{
			name: "empty",
			from: "",
			want: "On 2026-10-30 14:02, unknown sender wrote:",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := Attribution(tc.from, fixedTime); got != tc.want {
				t.Errorf("Attribution(%q) = %q, want %q", tc.from, got, tc.want)
			}
		})
	}
}

// replyOrigin is the message being replied to.
func replyOrigin() Compose {
	return Compose{
		From:       "Sender <sender@example.org>",
		To:         []string{"me@example.org", "other@example.org"},
		Cc:         []string{"cc@example.org", "Me <ME@example.org>"},
		Subject:    "the subject",
		Text:       "line one\n\nline three",
		MessageID:  "parent@example.org",
		References: []string{"root@example.org"},
	}
}

// TestReplyComposeTopPosting pins the layout: the reply first, the whole original
// quoted below it, blank lines quoted as a bare ">".
func TestReplyComposeTopPosting(t *testing.T) {
	got := ReplyCompose(replyOrigin(), ReplyOptions{
		From:        "Me <me@example.org>",
		Self:        "me@example.org",
		Body:        "my answer",
		Attribution: "On 2026-10-30 14:02, Sender <sender@example.org> wrote:",
	})

	want := "my answer\n" +
		"On 2026-10-30 14:02, Sender <sender@example.org> wrote:\n" +
		"\n" +
		"> line one\n" +
		">\n" +
		"> line three\n"

	if got.Text != want {
		t.Errorf("Text =\n%q\nwant\n%q", got.Text, want)
	}
	if got.Subject != "Re: the subject" {
		t.Errorf("Subject = %q, want %q", got.Subject, "Re: the subject")
	}
	if got.InReplyTo != "parent@example.org" {
		t.Errorf("InReplyTo = %q", got.InReplyTo)
	}
	if len(got.References) != 2 || got.References[1] != "parent@example.org" {
		t.Errorf("References = %v, want the original chain plus the parent", got.References)
	}
	if len(got.To) != 1 || got.To[0] != "Sender <sender@example.org>" {
		t.Errorf("To = %v, want only the original sender", got.To)
	}
	if len(got.Cc) != 0 {
		t.Errorf("Cc = %v, want none for a plain reply", got.Cc)
	}
}

// TestReplyComposeBottomPosting proves the quote comes first and the reply area last.
func TestReplyComposeBottomPosting(t *testing.T) {
	got := ReplyCompose(replyOrigin(), ReplyOptions{
		From:        "me@example.org",
		Self:        "me@example.org",
		Body:        "my answer",
		Attribution: "attrib",
		Bottom:      true,
	})

	if !strings.HasPrefix(got.Text, "attrib\n\n> line one") {
		t.Errorf("bottom-posting does not start with the quote: %q", got.Text)
	}
	if !strings.HasSuffix(got.Text, "my answer\n") {
		t.Errorf("bottom-posting does not end with the reply area: %q", got.Text)
	}
}

// TestReplyComposeSubjectDoesNotStack proves an existing prefix is reused, not repeated.
func TestReplyComposeSubjectDoesNotStack(t *testing.T) {
	for _, subject := range []string{"Re: the subject", "RE: the subject", "re: the subject"} {
		orig := replyOrigin()
		orig.Subject = subject

		got := ReplyCompose(orig, ReplyOptions{From: "me@example.org", Self: "me@example.org", Body: "x"})
		if got.Subject != subject {
			t.Errorf("Subject = %q from %q, want it unchanged", got.Subject, subject)
		}
	}

	empty := replyOrigin()
	empty.Subject = ""

	if got := ReplyCompose(empty, ReplyOptions{From: "me@example.org", Self: "me@example.org", Body: "x"}); got.Subject != "Re: " {
		t.Errorf("Subject = %q for an empty original", got.Subject)
	}
}

// TestReplyAllDropsSelf proves every other recipient stays, including display names,
// and the account's own address is removed case-insensitively from To and Cc.
func TestReplyAllDropsSelf(t *testing.T) {
	got := ReplyCompose(replyOrigin(), ReplyOptions{
		From: "me@example.org",
		Self: "me@example.org",
		Body: "answer",
		All:  true,
	})

	wantTo := []string{"Sender <sender@example.org>", "other@example.org"}
	if len(got.To) != len(wantTo) {
		t.Fatalf("To = %v, want %v", got.To, wantTo)
	}
	for i := range wantTo {
		if got.To[i] != wantTo[i] {
			t.Errorf("To[%d] = %q, want %q", i, got.To[i], wantTo[i])
		}
	}

	if len(got.Cc) != 1 || got.Cc[0] != "cc@example.org" {
		t.Errorf("Cc = %v, want only cc@example.org", got.Cc)
	}
}

// TestReplyComposeWithoutMessageID proves a reply to a message with no id still builds,
// without threading headers.
func TestReplyComposeWithoutMessageID(t *testing.T) {
	orig := replyOrigin()
	orig.MessageID = ""
	orig.References = nil

	got := ReplyCompose(orig, ReplyOptions{From: "me@example.org", Self: "me@example.org", Body: "answer"})
	if got.InReplyTo != "" {
		t.Errorf("InReplyTo = %q, want empty", got.InReplyTo)
	}
	if len(got.References) != 0 {
		t.Errorf("References = %v, want none", got.References)
	}
	if !strings.Contains(got.Text, "> line one") {
		t.Error("the original was not quoted")
	}
}

// TestForwardCompose proves a forward carries no quoting and no threading headers.
func TestForwardCompose(t *testing.T) {
	got := ForwardCompose(replyOrigin(), "me@example.org", "")

	if got.Subject != "Fwd: the subject" {
		t.Errorf("Subject = %q", got.Subject)
	}
	if strings.Contains(got.Text, "> line one") {
		t.Errorf("the forward quoted the original: %q", got.Text)
	}
	if !strings.Contains(got.Text, "---------- Forwarded message ----------") {
		t.Errorf("the forward has no separator: %q", got.Text)
	}
	if !strings.Contains(got.Text, "line one") {
		t.Errorf("the forward does not include the original: %q", got.Text)
	}
	if got.InReplyTo != "" || len(got.References) != 0 {
		t.Errorf("the forward carries threading headers: %q %v", got.InReplyTo, got.References)
	}
	if len(got.To) != 0 {
		t.Errorf("the forward pre-fills recipients: %v", got.To)
	}
	if got.From != "me@example.org" {
		t.Errorf("From = %q", got.From)
	}
}
