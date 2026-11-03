package tui

import (
	"strings"
	"testing"

	"github.com/emersion/go-imap/v2"
	"github.com/renderorange/chute/mail"
)

// threadedMessages is a three-message conversation: root, reply and reply-to-reply.
func threadedMessages() []mail.Message {
	messages := sampleMessages(3)
	messages[0].MessageID = "root@example.org"
	messages[1].MessageID = "reply@example.org"
	messages[1].InReplyTo = []string{"root@example.org"}
	messages[2].MessageID = "reply2@example.org"
	messages[2].InReplyTo = []string{"reply@example.org"}

	return messages
}

// TestCollapseHidesTheReplies covers the headline: z on a thread leaves one row, keeps
// the cursor on the root and records how many messages are hidden.
func TestCollapseHidesTheReplies(t *testing.T) {
	m := openInbox(t, withInbox(t, threadedMessages(), nil))

	if len(m.rows) != 3 {
		t.Fatalf("rows = %d, want 3 before collapsing", len(m.rows))
	}

	m = press(t, m, keyMsg("z"))

	if len(m.rows) != 1 {
		t.Fatalf("rows = %d, want the thread collapsed to one row", len(m.rows))
	}
	if got := m.rows[0].Hidden; got != 2 {
		t.Errorf("Hidden = %d, want 2", got)
	}
	if got := m.rows[0].Message.UID; got != 1 {
		t.Errorf("cursor message = %d, want the root", got)
	}
	if !strings.Contains(m.messageLines(1, 60)[0], "(2)") {
		t.Errorf("the collapsed row does not show the count: %q", m.messageLines(1, 60)[0])
	}
	if want := "collapsed thread"; m.status != want {
		t.Errorf("status = %q, want %q", m.status, want)
	}
}

// TestCollapseShowsUnreadDescendants keeps a collapsed row from hiding unread mail.
func TestCollapseShowsUnreadDescendants(t *testing.T) {
	messages := threadedMessages()
	messages[0].Flags = []imap.Flag{imap.FlagSeen} // the root is read; the replies are not.

	m := openInbox(t, withInbox(t, messages, nil))
	m = press(t, m, keyMsg("z"))

	if !m.rows[0].HiddenUnread {
		t.Error("the collapsed root does not record an unread descendant")
	}
}

// TestExpandRestoresTheThread proves the toggle is a toggle.
func TestExpandRestoresTheThread(t *testing.T) {
	m := openInbox(t, withInbox(t, threadedMessages(), nil))

	m = press(t, m, keyMsg("z"))
	m = press(t, m, keyMsg("z"))

	if len(m.rows) != 3 {
		t.Fatalf("rows = %d, want 3 after expanding", len(m.rows))
	}
	if want := "expanded thread"; m.status != want {
		t.Errorf("status = %q, want %q", m.status, want)
	}
}

// TestCollapseSingleMessageSaysSo avoids recording state that changes nothing.
func TestCollapseSingleMessageSaysSo(t *testing.T) {
	m := openInbox(t, withInbox(t, sampleMessages(1), nil))

	m = press(t, m, keyMsg("z"))

	if len(m.rows) != 1 {
		t.Errorf("rows = %d, want 1", len(m.rows))
	}
	if want := "thread has one message"; m.status != want {
		t.Errorf("status = %q, want %q", m.status, want)
	}
}

// TestCollapseIsIgnoredWhileFiltering keeps a filter match from disappearing inside a
// collapsed thread: collapse is suspended until the filter is cleared.
func TestCollapseIsIgnoredWhileFiltering(t *testing.T) {
	m := openInbox(t, withInbox(t, threadedMessages(), nil))
	m.filter = "subject"
	m.applyFilter()

	m = press(t, m, keyMsg("z"))

	if want := "clear the filter to collapse"; m.status != want {
		t.Errorf("status = %q, want %q", m.status, want)
	}
	if len(m.rows) != 3 {
		t.Errorf("rows = %d, want every match visible", len(m.rows))
	}
}

// TestCollapseResetsWhenTheFolderOpens proves the state belongs to one folder.
func TestCollapseResetsWhenTheFolderOpens(t *testing.T) {
	m := openInbox(t, withInbox(t, threadedMessages(), nil))

	m = press(t, m, keyMsg("z"))
	if len(m.rows) != 1 {
		t.Fatalf("rows = %d, want the thread collapsed", len(m.rows))
	}

	next, cmd := m.openFolderByName("INBOX", false)
	m = next.(Model)
	m = deliver(t, m, cmd)

	if len(m.rows) != 3 {
		t.Errorf("rows = %d, want the thread expanded in the freshly opened folder", len(m.rows))
	}
}

// TestCollapseFitsTheFrame pins the collapsed row at the minimum terminal size.
func TestCollapseFitsTheFrame(t *testing.T) {
	m := loaded(t, 60, 20, []mail.Folder{{Name: "INBOX", Delimiter: '/'}}, nil)
	m.loadMessages = fakeMessages(threadedMessages(), nil)
	m = openInboxAnyMode(t, m)

	m = press(t, m, keyMsg("z"))

	assertExactFrame(t, m, "collapsed thread")
}

// TestCollapseKeyOutsideTheListIsInert proves the triage guard applies.
func TestCollapseKeyOutsideTheListIsInert(t *testing.T) {
	m := loaded(t, 140, 40, []mail.Folder{{Name: "INBOX", Delimiter: '/'}}, nil)
	m.focus = focusFolders

	m = press(t, m, keyMsg("z"))

	if m.status != "" {
		t.Errorf("status = %q, want nothing", m.status)
	}
}
