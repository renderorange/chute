package tui

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/emersion/go-imap/v2"
	"github.com/renderorange/chute/config"
	"github.com/renderorange/chute/mail"
)

// fakeMessages returns a loader that always yields the given messages and error.
func fakeMessages(messages []mail.Message, err error) loadMessagesFunc {
	return func(config.Account, string) ([]mail.Message, error) {
		return messages, err
	}
}

// sampleMessages builds n messages with ascending UIDs 1..n.
func sampleMessages(n int) []mail.Message {
	messages := make([]mail.Message, 0, n)
	date := time.Date(2026, 10, 29, 9, 0, 0, 0, time.UTC)

	for i := 1; i <= n; i++ {
		messages = append(messages, mail.Message{
			UID:    imap.UID(i),
			SeqNum: uint32(i),
			Envelope: imap.Envelope{
				Subject: fmt.Sprintf("subject %02d", i),
				From:    []imap.Address{{Name: "Sender", Mailbox: "sender", Host: "example.org"}},
				Date:    date.Add(time.Duration(i) * time.Hour),
			},
		})
	}

	return messages
}

// withInbox returns a loaded model whose folder cursor sits on INBOX.
func withInbox(t *testing.T, messages []mail.Message, msgErr error) Model {
	t.Helper()

	m := loaded(t, 140, 40, []mail.Folder{{Name: "INBOX", Delimiter: '/'}}, nil)
	m.loadMessages = fakeMessages(messages, msgErr)

	return m
}

// openInbox presses enter on the folders panel and delivers the result.
func openInbox(t *testing.T, m Model) Model {
	t.Helper()

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)

	if cmd == nil {
		t.Fatal("enter returned no command")
	}

	next, _ = m.Update(cmd())

	return next.(Model)
}

func TestEnterOpensFolderAndLoadsMessages(t *testing.T) {
	m := withInbox(t, sampleMessages(3), nil)

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)

	if !m.msgLoading {
		t.Error("msgLoading = false, want true while the fetch runs")
	}
	if got, want := m.openFolder, "INBOX"; got != want {
		t.Errorf("openFolder = %q, want %q", got, want)
	}
	if got := m.focus; got != focusMessages {
		t.Errorf("focus = %v, want messages", got)
	}
	if cmd == nil {
		t.Fatal("enter returned no command")
	}

	next, _ = m.Update(cmd())
	m = next.(Model)

	if m.msgLoading {
		t.Error("msgLoading still true after the fetch returned")
	}
	if len(m.messages) != 3 {
		t.Fatalf("messages = %d, want 3", len(m.messages))
	}
	if got, want := m.messages[0].UID, imap.UID(3); got != want {
		t.Errorf("first message UID = %d, want newest %d", got, want)
	}
}

func TestMessageListRendersRows(t *testing.T) {
	m := openInbox(t, withInbox(t, sampleMessages(3), nil))
	view := m.View()

	for _, want := range []string{"subject 03", "subject 02", "Sender", "3 messages"} {
		if !strings.Contains(view, want) {
			t.Errorf("view missing %q", want)
		}
	}
}

func TestReaderShowsSelectedMessage(t *testing.T) {
	m := openInbox(t, withInbox(t, sampleMessages(2), nil))

	// Cursor starts on the newest message.
	if reader := strings.Join(m.readerLines(), "\n"); !strings.Contains(reader, "subject 02") {
		t.Errorf("reader missing the selected subject: %q", reader)
	}

	next, _ := m.Update(keyMsg("j"))
	m = next.(Model)

	if reader := strings.Join(m.readerLines(), "\n"); !strings.Contains(reader, "subject 01") {
		t.Errorf("reader did not follow the cursor to the older message: %q", reader)
	}
}

func TestMessageCursorClamps(t *testing.T) {
	m := openInbox(t, withInbox(t, sampleMessages(4), nil))

	for i := 0; i < 10; i++ {
		next, _ := m.Update(keyMsg("j"))
		m = next.(Model)
	}
	if got, want := m.messageCursor, 3; got != want {
		t.Errorf("cursor after j past the end = %d, want %d", got, want)
	}

	next, _ := m.Update(keyMsg("g"))
	if got := next.(Model).messageCursor; got != 0 {
		t.Errorf("cursor after g = %d, want 0", got)
	}

	last, _ := m.Update(keyMsg("G"))
	if got := last.(Model).messageCursor; got != 3 {
		t.Errorf("cursor after G = %d, want 3", got)
	}
}

func TestMessageScrollFollowsCursor(t *testing.T) {
	// A short terminal fits only a few rows.
	m := loaded(t, 140, 14, []mail.Folder{{Name: "INBOX", Delimiter: '/'}}, nil)
	m.loadMessages = fakeMessages(sampleMessages(40), nil)
	m = openInbox(t, m)

	for i := 0; i < 20; i++ {
		next, _ := m.Update(keyMsg("j"))
		m = next.(Model)
	}

	if m.messageScroll == 0 {
		t.Error("scroll stayed at 0 after moving past the visible rows")
	}
	if m.messageCursor >= m.messageScroll+m.messageRows()+1 {
		t.Errorf("cursor %d is outside the window starting at %d", m.messageCursor, m.messageScroll)
	}
}

func TestEnterOnContainerFolderIsRefused(t *testing.T) {
	m := loaded(t, 140, 40, []mail.Folder{
		{Name: "Work", Delimiter: '/', Attrs: []imap.MailboxAttr{imap.MailboxAttrNoSelect}},
	}, nil)
	m.loadMessages = fakeMessages(sampleMessages(2), nil)

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)

	if cmd != nil {
		t.Error("enter on a container folder returned a command")
	}
	if m.openFolder != "" {
		t.Errorf("openFolder = %q, want empty", m.openFolder)
	}
	if !strings.Contains(m.status, "container") {
		t.Errorf("status = %q, want an explanation", m.status)
	}
}

func TestStaleMessagesMessageIgnored(t *testing.T) {
	m := withInbox(t, sampleMessages(2), nil)
	m.openFolder = "INBOX"
	m.msgLoading = true

	next, _ := m.Update(messagesMsg{account: m.accountName(), folder: "Other", messages: sampleMessages(5)})
	got := next.(Model)

	if len(got.messages) != 0 {
		t.Errorf("messages = %d, want 0 for a stale folder", len(got.messages))
	}
	if !got.msgLoading {
		t.Error("stale message cleared the loading flag")
	}
}

func TestMessageLoadError(t *testing.T) {
	m := withInbox(t, nil, errors.New("fetch failed"))
	m = openInbox(t, m)

	if len(m.messages) != 0 {
		t.Errorf("messages = %d, want 0", len(m.messages))
	}
	if !strings.Contains(m.status, "fetch failed") {
		t.Errorf("status = %q, want the loader error", m.status)
	}
}

func TestReloadClearsOpenFolder(t *testing.T) {
	m := openInbox(t, withInbox(t, sampleMessages(2), nil))

	next, _ := m.Update(keyMsg("r"))
	got := next.(Model)

	if got.openFolder != "" {
		t.Errorf("openFolder = %q after reload, want empty", got.openFolder)
	}
	if len(got.messages) != 0 {
		t.Errorf("messages = %d after reload, want 0", len(got.messages))
	}
}
