package mail

import (
	"strings"
	"testing"

	"github.com/emersion/go-imap/v2"
)

// TestParseBodyKeepsTheReferencesChain is the defect in #24 at the mail layer: the
// full body fetch already carries the top-level headers, so the ancestor chain is
// in hand and was being thrown away.
func TestParseBodyKeepsTheReferencesChain(t *testing.T) {
	raw := "From: sender@example.com\r\n" +
		"Subject: re: the thread\r\n" +
		"Message-ID: <parent@example.com>\r\n" +
		"In-Reply-To: <grandparent@example.com>\r\n" +
		"References: <root@example.com> <grandparent@example.com>\r\n" +
		"\r\n" +
		"the text\r\n"

	body, err := parseBody([]byte(raw))
	if err != nil {
		t.Fatalf("parseBody: %v", err)
	}

	want := []string{"<root@example.com>", "<grandparent@example.com>"}
	if got := strings.Join(body.References, " "); got != strings.Join(want, " ") {
		t.Errorf("References = %q, want %q", got, strings.Join(want, " "))
	}
	if !strings.Contains(body.Text, "the text") {
		t.Errorf("Text = %q", body.Text)
	}
}

// TestParseBodyWithoutReferencesIsNotAnError covers the issue's decision that a
// message with no References header is ordinary mail, not a parse failure.
func TestParseBodyWithoutReferencesIsNotAnError(t *testing.T) {
	raw := "From: sender@example.com\r\n" +
		"Subject: a root message\r\n" +
		"\r\n" +
		"no threading headers here\r\n"

	body, err := parseBody([]byte(raw))
	if err != nil {
		t.Fatalf("parseBody: %v", err)
	}
	if len(body.References) != 0 {
		t.Errorf("References = %v, want none", body.References)
	}
}

// TestFetchBodyCarriesReferencesInOneFetch is the issue's cost criterion. The body
// fetch asks for the whole message (BODY.PEEK[]), so References arrives in the
// command that was already being sent: the fix costs no extra FETCH. Counted from
// the real client's protocol log rather than asserted in prose.
func TestFetchBodyCarriesReferencesInOneFetch(t *testing.T) {
	const password = "s3cret-pass"

	server := newTestServerWith(t, testCaps(), "tester", password)
	log := &syncBuffer{}

	client, err := connectWith(dialInsecure, server.account(), password, WithDebugWriter(log))
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer client.Close()

	raw := "From: sender@example.com\r\n" +
		"To: tester@example.com\r\n" +
		"Subject: re: the thread\r\n" +
		"Message-ID: <parent@example.com>\r\n" +
		"References: <root@example.com> <grandparent@example.com>\r\n" +
		"\r\n" +
		"the text\r\n"

	data, err := server.user.Append("INBOX", strings.NewReader(raw), &imap.AppendOptions{})
	if err != nil {
		t.Fatalf("append: %v", err)
	}

	body, err := client.FetchBody("INBOX", data.UID)
	if err != nil {
		t.Fatalf("FetchBody: %v", err)
	}

	want := []string{"<root@example.com>", "<grandparent@example.com>"}
	if got := strings.Join(body.References, " "); got != strings.Join(want, " ") {
		t.Errorf("References through FetchBody = %q, want %q", got, strings.Join(want, " "))
	}

	if got := countClientFetches(log.String()); got != 1 {
		t.Errorf("FetchBody sent %d FETCH commands, want 1:\n%s", got, log.String())
	}
}

// countClientFetches counts the FETCH commands the client sent, from a protocol log.
// Client command lines are "<tag> FETCH ..." or "<tag> UID FETCH ..."; server responses
// carry "OK"/"NO"/"BAD" in that position and untagged data starts with "*".
func countClientFetches(protocolLog string) int {
	count := 0

	for _, line := range strings.Split(protocolLog, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}

		switch {
		case fields[1] == "FETCH":
			count++
		case len(fields) > 2 && fields[1] == "UID" && fields[2] == "FETCH":
			count++
		}
	}

	return count
}
