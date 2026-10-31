package mail

import (
	"strings"
	"testing"

	"github.com/emersion/go-imap/v2"
	"github.com/renderorange/chute/config"
)

// TestFetchBodyAgainstInProcessServer exercises the wrapper the TUI calls: it must
// return the text and the attachment through the real client, not just through
// parseBody.
func TestFetchBodyAgainstInProcessServer(t *testing.T) {
	server := newTestServer(t, testCaps())
	client := server.connect(t)

	raw := "From: sender@example.com\r\n" +
		"To: tester@example.com\r\n" +
		"Subject: with attachment\r\n" +
		"MIME-Version: 1.0\r\n" +
		"Content-Type: multipart/mixed; boundary=\"BOUND\"\r\n" +
		"\r\n" +
		"--BOUND\r\n" +
		"Content-Type: text/plain; charset=utf-8\r\n" +
		"\r\n" +
		"the text part\r\n" +
		"--BOUND\r\n" +
		"Content-Type: application/octet-stream\r\n" +
		"Content-Disposition: attachment; filename=\"notes.txt\"\r\n" +
		"\r\n" +
		"attachment bytes\r\n" +
		"--BOUND--\r\n"

	data, err := server.user.Append("INBOX", strings.NewReader(raw), &imap.AppendOptions{})
	if err != nil {
		t.Fatalf("append: %v", err)
	}

	body, err := client.FetchBody("INBOX", data.UID)
	if err != nil {
		t.Fatalf("FetchBody: %v", err)
	}

	if !strings.Contains(body.Text, "the text part") {
		t.Errorf("Text = %q", body.Text)
	}
	if len(body.Attachments) != 1 {
		t.Fatalf("attachments = %d, want 1", len(body.Attachments))
	}
	if body.Attachments[0].Filename != "notes.txt" {
		t.Errorf("filename = %q", body.Attachments[0].Filename)
	}
	if got := string(body.Attachments[0].Data); !strings.Contains(got, "attachment bytes") {
		t.Errorf("attachment data = %q", got)
	}
}

// TestAppendToMissingFolderReportsError covers the error path of Append: a mailbox that
// does not exist must fail, not silently succeed.
func TestAppendToMissingFolderReportsError(t *testing.T) {
	server := newTestServer(t, testCaps())
	client := server.connect(t)

	if _, _, err := client.Append("NoSuchFolder", nil, []byte("A1\r\n\r\nbody")); err == nil {
		t.Fatal("Append succeeded against a mailbox that does not exist")
	}
}

// TestSaveDraftRefusesEmptyFolder proves the session guard, so a missing drafts folder
// is reported rather than sent to the server as an empty mailbox name.
func TestSaveDraftRefusesEmptyFolder(t *testing.T) {
	client := &fakeClient{}
	session := newSession(config.Account{Name: "memory"}, func(config.Account) (sessionClient, error) {
		return client, nil
	})

	if _, _, err := session.SaveDraft("  ", []byte("msg")); err == nil {
		t.Fatal("SaveDraft accepted an empty folder")
	}
	if len(client.appendCalls) != 0 {
		t.Errorf("SaveDraft reached the server with %v", client.appendCalls)
	}
}
