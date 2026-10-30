package tui

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/emersion/go-imap/v2"
	"github.com/renderorange/chute/config"
	"github.com/renderorange/chute/mail"
)

// fakeBody returns a loader that always yields the given body and error.
func fakeBody(body *mail.Body, err error) loadBodyFunc {
	return func(config.Account, string, imap.UID) (*mail.Body, error) {
		return body, err
	}
}

// sampleBody is a message with text and two attachments.
func sampleBody() *mail.Body {
	return &mail.Body{
		Text: "hello body\nsecond line",
		Attachments: []mail.Attachment{
			{Filename: "report.pdf", ContentType: "application/pdf", Data: []byte("pdf")},
			{Filename: "chart.png", ContentType: "image/png", Data: []byte("png")},
		},
	}
}

// loadedBody opens INBOX with a fake body loader installed.
func loadedBody(t *testing.T, messages []mail.Message, body *mail.Body) Model {
	t.Helper()

	m := openInbox(t, withInbox(t, messages, nil))
	m.loadBody = fakeBody(body, nil)

	return m
}

// openSelectedBody presses enter on the messages panel and delivers the result.
func openSelectedBody(t *testing.T, m Model) Model {
	t.Helper()

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)

	if cmd == nil {
		t.Fatal("enter returned no command")
	}

	next, _ = m.Update(cmd())

	return next.(Model)
}

func TestEnterLoadsBodyAndKeepsListFocus(t *testing.T) {
	m := openSelectedBody(t, loadedBody(t, sampleMessages(2), sampleBody()))

	if m.focus != focusMessages {
		t.Errorf("focus = %v, want messages so the list keeps navigating", m.focus)
	}

	key := bodyKey{account: "fastmail", folder: "INBOX", uid: m.messages[0].UID}
	if _, ok := m.bodies[key]; !ok {
		t.Fatal("body was not cached for the selected message")
	}

	view := m.View()
	for _, want := range []string{"hello body", "second line"} {
		if !strings.Contains(view, want) {
			t.Errorf("view missing body text %q", want)
		}
	}
}

func TestCachedBodyIsNotFetchedTwice(t *testing.T) {
	m := openSelectedBody(t, loadedBody(t, sampleMessages(1), sampleBody()))

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		t.Error("enter on a cached body returned a fetch command")
	}
	if got := next.(Model).focus; got != focusMessages {
		t.Errorf("focus = %v, want messages", got)
	}
}

func TestReaderListsAttachments(t *testing.T) {
	m := openSelectedBody(t, loadedBody(t, sampleMessages(1), sampleBody()))
	view := m.View()

	for _, want := range []string{"attachments", "report.pdf", "chart.png"} {
		if !strings.Contains(view, want) {
			t.Errorf("view missing %q", want)
		}
	}
}

func TestReaderScrollsBody(t *testing.T) {
	body := &mail.Body{Text: strings.Repeat("body line\n", 200)}
	m := openSelectedBody(t, loadedBody(t, sampleMessages(1), body))

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = next.(Model)
	if m.focus != focusReader {
		t.Fatalf("focus = %v after tab, want reader", m.focus)
	}

	for i := 0; i < 5; i++ {
		next, _ := m.Update(keyMsg("j"))
		m = next.(Model)
	}
	if m.readerScroll == 0 {
		t.Fatal("reader did not scroll after j")
	}

	scrolled := m.readerScroll
	next, _ = m.Update(keyMsg("k"))
	m = next.(Model)
	if m.readerScroll != scrolled-1 {
		t.Errorf("scroll after k = %d, want %d", m.readerScroll, scrolled-1)
	}

	next, _ = m.Update(keyMsg("g"))
	m = next.(Model)
	if m.readerScroll != 0 {
		t.Errorf("scroll after g = %d, want 0", m.readerScroll)
	}
}

func TestMovingMessageCursorResetsReaderScroll(t *testing.T) {
	body := &mail.Body{Text: strings.Repeat("body line\n", 200)}
	m := openSelectedBody(t, loadedBody(t, sampleMessages(2), body))

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = next.(Model)
	for i := 0; i < 5; i++ {
		next, _ := m.Update(keyMsg("j"))
		m = next.(Model)
	}
	if m.readerScroll == 0 {
		t.Fatal("reader did not scroll")
	}

	m.focus = focusMessages
	next, _ = m.Update(keyMsg("j"))
	m = next.(Model)

	if m.readerScroll != 0 {
		t.Errorf("readerScroll = %d after moving the message cursor, want 0", m.readerScroll)
	}
}

func TestBodyLoadErrorSetsStatus(t *testing.T) {
	m := openInbox(t, withInbox(t, sampleMessages(1), nil))
	m.loadBody = fakeBody(nil, errors.New("fetch failed"))

	m = openSelectedBody(t, m)

	if len(m.bodies) != 0 {
		t.Errorf("bodies cached = %d, want 0 after a failed load", len(m.bodies))
	}
	if !strings.Contains(m.status, "fetch failed") {
		t.Errorf("status = %q, want the loader error", m.status)
	}
}

func TestStaleBodyMessageIgnored(t *testing.T) {
	m := loadedBody(t, sampleMessages(1), sampleBody())

	next, _ := m.Update(bodyMsg{account: "someone-else", folder: "INBOX", uid: 1, body: sampleBody()})
	if got := len(next.(Model).bodies); got != 0 {
		t.Errorf("bodies cached = %d, want 0 for a stale account", got)
	}
}

func TestAttachmentModalOpens(t *testing.T) {
	m := openSelectedBody(t, loadedBody(t, sampleMessages(1), sampleBody()))

	next, _ := m.Update(keyMsg("s"))
	m = next.(Model)

	if !m.attachOpen {
		t.Fatal("s did not open the attachment modal")
	}
	if view := m.View(); !strings.Contains(view, "save attachment") {
		t.Errorf("view missing the modal title: %q", view)
	}
}

func TestAttachmentModalMovesCursor(t *testing.T) {
	m := openSelectedBody(t, loadedBody(t, sampleMessages(1), sampleBody()))
	next, _ := m.Update(keyMsg("s"))
	m = next.(Model)

	next, _ = m.Update(keyMsg("j"))
	m = next.(Model)
	if got, want := m.attachCursor, 1; got != want {
		t.Errorf("attachment cursor = %d, want %d", got, want)
	}

	next, _ = m.Update(keyMsg("j"))
	m = next.(Model)
	if got, want := m.attachCursor, 1; got != want {
		t.Errorf("attachment cursor past the end = %d, want %d", got, want)
	}
}

func TestAttachmentModalSavesSelectedFile(t *testing.T) {
	m := openSelectedBody(t, loadedBody(t, sampleMessages(1), sampleBody()))
	m.cfg.General.DownloadDir = t.TempDir()

	next, _ := m.Update(keyMsg("s"))
	m = next.(Model)

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)
	if cmd == nil {
		t.Fatal("enter in the attachment modal returned no command")
	}

	next, _ = m.Update(cmd())
	m = next.(Model)

	path := filepath.Join(m.cfg.General.DownloadDir, "report.pdf")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("saved file missing: %v", err)
	}
	if !strings.Contains(m.status, "saved") {
		t.Errorf("status = %q, want a save confirmation", m.status)
	}
	if m.attachOpen {
		t.Error("modal stayed open after a save")
	}
}

func TestAttachmentModalClosesOnEscape(t *testing.T) {
	m := openSelectedBody(t, loadedBody(t, sampleMessages(1), sampleBody()))
	next, _ := m.Update(keyMsg("s"))
	m = next.(Model)

	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if next.(Model).attachOpen {
		t.Error("esc did not close the attachment modal")
	}
}

func TestAttachmentModalSavesEachAttachment(t *testing.T) {
	m := openSelectedBody(t, loadedBody(t, sampleMessages(1), sampleBody()))
	m.cfg.General.DownloadDir = t.TempDir()

	// Save the first attachment.
	next, _ := m.Update(keyMsg("s"))
	m = next.(Model)
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)
	next, _ = m.Update(cmd())
	m = next.(Model)

	// Open the picker again, choose the second attachment, and save it.
	next, _ = m.Update(keyMsg("s"))
	m = next.(Model)
	next, _ = m.Update(keyMsg("j"))
	m = next.(Model)
	next, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)
	next, _ = m.Update(cmd())
	m = next.(Model)

	for _, name := range []string{"report.pdf", "chart.png"} {
		if _, err := os.Stat(filepath.Join(m.cfg.General.DownloadDir, name)); err != nil {
			t.Errorf("%s was not saved: %v", name, err)
		}
	}
}

func TestAttachmentKeyWithoutLoadedBody(t *testing.T) {
	m := openInbox(t, withInbox(t, sampleMessages(1), nil))

	next, _ := m.Update(keyMsg("s"))
	m = next.(Model)

	if m.attachOpen {
		t.Error("s opened the modal before the body was loaded")
	}
	if m.status == "" {
		t.Error("s produced no status message")
	}
}

func TestAttachmentSavedErrorSetsStatus(t *testing.T) {
	m := openSelectedBody(t, loadedBody(t, sampleMessages(1), sampleBody()))

	next, _ := m.Update(attachmentSavedMsg{err: errors.New("disk full")})
	m = next.(Model)

	if !strings.Contains(m.status, "disk full") {
		t.Errorf("status = %q, want the save error", m.status)
	}
}

func TestWrapText(t *testing.T) {
	tests := []struct {
		name  string
		in    string
		width int
		want  []string
	}{
		{"short", "hello", 10, []string{"hello"}},
		{"exact", "hello", 5, []string{"hello"}},
		{"wraps at spaces", "hello world", 6, []string{"hello", "world"}},
		{"breaks a long word", "abcdefgh", 4, []string{"abcd", "efgh"}},
		{"keeps paragraphs", "one\n\ntwo", 10, []string{"one", "", "two"}},
		{"normalizes crlf", "one\r\ntwo", 10, []string{"one", "two"}},
		{"expands tabs", "a\tb", 8, []string{"a    b"}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := wrapText(tc.in, tc.width)
			if strings.Join(got, "|") != strings.Join(tc.want, "|") {
				t.Errorf("wrapText(%q, %d) = %q, want %q", tc.in, tc.width, got, tc.want)
			}
		})
	}
}

func TestFormatSize(t *testing.T) {
	tests := []struct {
		in   int
		want string
	}{
		{0, "0 B"},
		{1023, "1023 B"},
		{1024, "1.0 KB"},
		{1536, "1.5 KB"},
		{1024 * 1024, "1.0 MB"},
	}

	for _, tc := range tests {
		if got := formatSize(tc.in); got != tc.want {
			t.Errorf("formatSize(%d) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
