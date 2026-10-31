package mail

import (
	"bytes"
	"strings"
	"testing"
)

// The debug log exists so a gate can be checked, and login is the IMAP LOGIN
// command, so the password travels through the same stream. These tests run the
// real encoder against the in-process server: the point is to observe what
// imapclient actually emits, not what a fake of it would emit.

// TestDebugWriterRedactsPassword proves the protocol log shows the commands and
// never the credential.
func TestDebugWriterRedactsPassword(t *testing.T) {
	const password = "s3cret-pass"

	server := newTestServerWith(t, testCaps(), "tester", password)
	log := &syncBuffer{}

	client, err := connectWith(dialInsecure, server.account(), password, WithDebugWriter(log))
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer client.Close()

	if _, err := client.Folders(); err != nil {
		t.Fatalf("Folders: %v", err)
	}

	text := log.String()
	if text == "" {
		t.Fatal("the debug writer received no protocol traffic")
	}
	if !strings.Contains(text, "LOGIN") {
		t.Errorf("the log does not show the LOGIN command:\n%s", text)
	}
	if strings.Contains(text, password) {
		t.Errorf("the credential reached the debug log:\n%s", text)
	}
	if !strings.Contains(text, redacted) {
		t.Errorf("the log does not show the masking marker %q:\n%s", redacted, text)
	}
}

// TestDebugWriterRedactsEscapedPassword covers the other encoding the encoder can
// choose: a password containing the characters a quoted string has to escape.
func TestDebugWriterRedactsEscapedPassword(t *testing.T) {
	const password = `qu"ote\pass`

	server := newTestServerWith(t, testCaps(), "tester", password)
	log := &syncBuffer{}

	client, err := connectWith(dialInsecure, server.account(), password, WithDebugWriter(log))
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer client.Close()

	text := log.String()
	if text == "" {
		t.Fatal("the debug writer received no protocol traffic")
	}

	// The raw form and the RFC 3501 quoted form, escaped.
	for _, want := range []string{password, `qu\"ote\\pass`} {
		if strings.Contains(text, want) {
			t.Errorf("the credential reached the debug log as %q:\n%s", want, text)
		}
	}
	if !strings.Contains(text, redacted) {
		t.Errorf("the log does not show the masking marker %q:\n%s", redacted, text)
	}
}

// TestRedactWriterMasksBothForms pins the replacement itself, including a secret
// that contains the other secret as a prefix.
func TestRedactWriterMasksBothForms(t *testing.T) {
	var buf bytes.Buffer
	w := newRedactWriter(&buf, `pa"ss\word`)

	stream := "LOGIN \"tester\" \"pa\\\"ss\\\\word\"\r\n" +
		"A1 LITERAL {11}\r\npa\"ss\\word\r\n"

	if _, err := w.Write([]byte(stream)); err != nil {
		t.Fatalf("Write: %v", err)
	}

	got := buf.String()
	if strings.Contains(got, `pa"ss\word`) || strings.Contains(got, `pa\"ss\\word`) {
		t.Errorf("the credential survived the writer:\n%s", got)
	}
	if got := strings.Count(got, redacted); got != 2 {
		t.Errorf("masked %d occurrences, want 2:\n%s", got, buf.String())
	}
	if !strings.Contains(got, "LOGIN \"tester\"") {
		t.Errorf("the writer changed the rest of the line:\n%s", got)
	}
}

// TestManagerPassesDebugWriter proves the TUI's path is covered too: the manager,
// not just a direct Connect, hands the writer to every account connection.
func TestManagerPassesDebugWriter(t *testing.T) {
	const password = "s3cret-pass"

	server := newTestServerWith(t, testCaps(), "tester", password)
	log := &syncBuffer{}

	acct := server.account()
	acct.IMAP.Password = password

	manager := NewManager(WithDebugWriter(log), withDial(dialInsecure))
	defer manager.Close()

	if _, err := manager.For(acct).Folders(); err != nil {
		t.Fatalf("Folders: %v", err)
	}

	text := log.String()
	if text == "" {
		t.Fatal("the manager's connection did not write to the debug writer")
	}
	if !strings.Contains(text, "LOGIN") {
		t.Errorf("the log does not show the LOGIN command:\n%s", text)
	}
	if strings.Contains(text, password) {
		t.Errorf("the credential reached the debug log:\n%s", text)
	}
}

// TestRedactWriterEmptyPassword leaves the stream alone, so an account with no
// resolved credential cannot turn every string into a match.
func TestRedactWriterEmptyPassword(t *testing.T) {
	var buf bytes.Buffer
	w := newRedactWriter(&buf, "")

	if _, err := w.Write([]byte("A1 CAPABILITY\r\n")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if got, want := buf.String(), "A1 CAPABILITY\r\n"; got != want {
		t.Errorf("stream = %q, want %q", got, want)
	}
}
