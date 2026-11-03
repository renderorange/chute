package mail

import (
	"strings"
	"testing"

	"github.com/emersion/go-imap/v2"
)

// appendReferencesMessage appends a message whose References header is given verbatim, so a
// folded value can be exercised as well as a plain one (#70).
func (s *testServer) appendReferencesMessage(t *testing.T, mailbox, subject, messageID, references string) imap.UID {
	t.Helper()

	raw := "From: sender@example.com\r\n" +
		"To: tester@example.com\r\n" +
		"Subject: " + subject + "\r\n" +
		"Message-ID: <" + messageID + ">\r\n" +
		"References: " + references + "\r\n" +
		"Date: Mon, 02 Jan 2006 15:04:05 -0700\r\n" +
		"\r\n" +
		"body\r\n"

	data, err := s.user.Append(mailbox, strings.NewReader(raw), &imap.AppendOptions{})
	if err != nil {
		t.Fatalf("append to %s: %v", mailbox, err)
	}

	return data.UID
}

// envelopeWithSubject returns the fetched message with the given subject.
func envelopeWithSubject(t *testing.T, messages []Message, subject string) Message {
	t.Helper()

	for _, msg := range messages {
		if msg.Subject() == subject {
			return msg
		}
	}

	t.Fatalf("no message with subject %q in %d messages", subject, len(messages))

	return Message{}
}

// TestFetchEnvelopesCarriesReferencesInOneFetch is #70's cost claim: the folder load still
// issues exactly one FETCH, with the header section inside it, and the ids arrive bare so
// they can be compared with envelope ids.
func TestFetchEnvelopesCarriesReferencesInOneFetch(t *testing.T) {
	server := newTestServer(t, testCaps())
	log := &syncBuffer{}

	client, err := connectWith(dialInsecure, server.account(), "secret", WithDebugWriter(log))
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer client.Close()

	server.appendReferencesMessage(t, "INBOX", "root", "root@example.com", "<root@example.com>")
	server.appendReferencesMessage(t, "INBOX", "reply", "reply@example.com", "<root@example.com> <grand@example.com>")

	messages, err := client.FetchEnvelopes("INBOX")
	if err != nil {
		t.Fatalf("FetchEnvelopes: %v", err)
	}

	reply := envelopeWithSubject(t, messages, "reply")
	if got, want := strings.Join(reply.References, " "), "root@example.com grand@example.com"; got != want {
		t.Errorf("References = %q, want %q", got, want)
	}

	if got := countClientFetches(log.String()); got != 1 {
		t.Errorf("the folder load sent %d FETCH commands, want 1", got)
	}
	for _, want := range []string{"HEADER.FIELDS", "REFERENCES"} {
		if !strings.Contains(strings.ToUpper(log.String()), want) {
			t.Errorf("the FETCH does not ask for %q:\n%s", want, log.String())
		}
	}
}

// TestFetchEnvelopesUnfoldsReferences proves a folded header, which a long chain produces,
// parses into the same bare ids.
func TestFetchEnvelopesUnfoldsReferences(t *testing.T) {
	server := newTestServer(t, testCaps())
	client := server.connect(t)

	uid := server.appendReferencesMessage(t, "INBOX", "folded", "folded@example.com",
		"<one@example.com>,\r\n <two@example.com>,\r\n <three@example.com>")
	_ = uid

	messages, err := client.FetchEnvelopes("INBOX")
	if err != nil {
		t.Fatalf("FetchEnvelopes: %v", err)
	}

	msg := envelopeWithSubject(t, messages, "folded")
	want := "one@example.com two@example.com three@example.com"
	if got := strings.Join(msg.References, " "); got != want {
		t.Errorf("References = %q, want %q", got, want)
	}
}

// TestPollCarriesReferences proves the incremental path fetches the header too, so a reply
// that arrives while the folder is open threads the same way as one that was there at load.
func TestPollCarriesReferences(t *testing.T) {
	server := newTestServer(t, testCaps())
	session, client := testSession(t, server)

	server.appendReferencesMessage(t, "INBOX", "root", "root@example.com", "<root@example.com>")

	result, err := session.Poll("INBOX", 0)
	if err != nil {
		t.Fatalf("Poll: %v", err)
	}

	if len(result.Messages) != 1 {
		t.Fatalf("poll returned %d messages, want 1", len(result.Messages))
	}
	if got, want := strings.Join(result.Messages[0].References, " "), "root@example.com"; got != want {
		t.Errorf("polled References = %q, want %q", got, want)
	}

	_ = client
}

// TestParseReferencesHeaderHandlesAMissingName covers a server that returns the value without
// the header name; the parser keeps the ids either way.
func TestParseReferencesHeaderHandlesAMissingName(t *testing.T) {
	for _, raw := range []string{
		"References: <a@example.com> <b@example.com>\r\n",
		"<a@example.com> <b@example.com>\r\n",
	} {
		if got, want := strings.Join(parseReferencesHeader([]byte(raw)), " "), "a@example.com b@example.com"; got != want {
			t.Errorf("parseReferencesHeader(%q) = %q, want %q", raw, got, want)
		}
	}
}
