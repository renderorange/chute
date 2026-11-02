package tui

import (
	"errors"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/emersion/go-imap/v2"
	"github.com/renderorange/chute/config"
	"github.com/renderorange/chute/mail"
)

// openFolderModel returns a model showing two messages in INBOX with a
// high-water mark at the newest UID.
func openFolderModel(t *testing.T) Model {
	t.Helper()

	m := sized(140, 40)
	m.account = 0
	m.openFolder = "INBOX"
	m.messages = sortNewestFirst(sampleMessages(2))
	m.rows = BuildRows(m.messages)
	m.lastSeen = map[folderKey]imap.UID{{account: "fastmail", folder: "INBOX"}: 2}

	return m
}

// pollResultMsg wraps a poll result for the active account and folder.
func pollResultMsg(result mail.PollResult) pollMsg {
	return pollMsg{account: "fastmail", folder: "INBOX", result: result}
}

func TestInitArmsThePollAtTheConfiguredInterval(t *testing.T) {
	cfg := testConfig()
	cfg.General.PollInterval = 5

	m := New(cfg, "test")

	var interval time.Duration
	var fire func(time.Time) tea.Msg
	m.clock = func(d time.Duration, fn func(time.Time) tea.Msg) tea.Cmd {
		interval = d
		fire = fn

		return func() tea.Msg { return nil }
	}

	cmd := m.Init()
	if cmd == nil {
		t.Fatal("Init returned no command")
	}

	batch, ok := cmd().(tea.BatchMsg)
	if !ok {
		t.Fatalf("Init produced %T, want a batch of the folder load and the tick", cmd())
	}
	if len(batch) != 2 {
		t.Errorf("Init batch holds %d commands, want 2", len(batch))
	}
	if interval != 5*time.Second {
		t.Errorf("poll interval = %v, want 5s from the config", interval)
	}
	if fire == nil {
		t.Fatal("Init did not hand the clock a fire function")
	}
	if _, ok := fire(time.Now()).(pollTickMsg); !ok {
		t.Errorf("the tick produced %T, want pollTickMsg", fire(time.Now()))
	}
}

func TestPollTickStartsAPollForTheOpenFolder(t *testing.T) {
	m := openFolderModel(t)

	var gotFolder string
	var gotSince imap.UID
	m.pollFolder = func(acct config.Account, folder string, since imap.UID) (mail.PollResult, error) {
		gotFolder = folder
		gotSince = since

		return mail.PollResult{}, nil
	}

	_, cmd := m.Update(pollTickMsg{})
	if cmd == nil {
		t.Fatal("a tick started no poll")
	}

	msg, ok := cmd().(pollMsg)
	if !ok {
		t.Fatalf("the poll produced %T, want pollMsg", cmd())
	}
	if gotFolder != "INBOX" {
		t.Errorf("polled folder = %q, want INBOX", gotFolder)
	}
	if gotSince != 2 {
		t.Errorf("polled since = %d, want the high-water mark 2", gotSince)
	}
	if msg.account != "fastmail" {
		t.Errorf("poll message account = %q, want fastmail", msg.account)
	}
}

func TestPollTickWithoutAnOpenFolderDoesNothing(t *testing.T) {
	m := sized(140, 40)

	if _, cmd := m.Update(pollTickMsg{}); cmd != nil {
		t.Error("a tick with no folder open started a poll")
	}
}

func TestPollTickIsSuspendedByDialogs(t *testing.T) {
	tests := []struct {
		name  string
		apply func(*Model)
	}{
		{"account selector", func(m *Model) { m.accountsOpen = true }},
		{"delete confirmation", func(m *Model) { m.confirmOpen = true }},
		{"attachments", func(m *Model) { m.attachOpen = true }},
		{"folders overlay", func(m *Model) { m.foldersOpen = true }},
		{"composer", func(m *Model) { m.compose = &composer{} }},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := openFolderModel(t)
			m.pollFolder = func(config.Account, string, imap.UID) (mail.PollResult, error) {
				t.Error("a poll ran while a dialog was open")
				return mail.PollResult{}, nil
			}
			tc.apply(&m)

			if _, cmd := m.Update(pollTickMsg{}); cmd != nil {
				t.Error("a tick started a poll while a dialog was open")
			}
		})
	}
}

func TestPollMergesNewMessagesAndUpdatesFlags(t *testing.T) {
	m := openFolderModel(t)

	read := sampleMessages(1)[0] // UID 1
	read.Flags = []imap.Flag{imap.FlagSeen}
	fresh := sampleMessages(3)[2] // UID 3, newest date

	next, cmd := m.Update(pollResultMsg(mail.PollResult{
		Messages: []mail.Message{read, fresh},
		Total:    3,
		Unseen:   2,
	}))

	got := next.(Model)
	if cmd != nil {
		t.Error("a poll whose count grew asked for a reconcile")
	}
	if len(got.messages) != 3 {
		t.Fatalf("messages = %d, want 3", len(got.messages))
	}
	if got.messages[0].UID != 3 {
		t.Errorf("newest row UID = %d, want 3", got.messages[0].UID)
	}

	seen := false
	for _, msg := range got.messages {
		if msg.UID == 1 {
			seen = msg.Seen()
		}
	}
	if !seen {
		t.Error("the polled \\Seen flag was not applied to the existing row")
	}
	if got.openUnseen != 2 {
		t.Errorf("openUnseen = %d, want 2", got.openUnseen)
	}
}

func TestPollInsertsAReplyInsideItsThread(t *testing.T) {
	m := sized(140, 40)
	m.openFolder = "INBOX"
	m.messages = sortNewestFirst([]mail.Message{threadMessage(1, "parent", "<m1>", 1)})
	m.rows = BuildRows(m.messages)
	m.lastSeen = map[folderKey]imap.UID{{account: "fastmail", folder: "INBOX"}: 1}

	reply := threadMessage(2, "re: parent", "<m2>", 2, "<m1>")

	next, _ := m.Update(pollResultMsg(mail.PollResult{Messages: []mail.Message{reply}, Total: 2}))
	got := next.(Model)

	if len(got.rows) != 2 {
		t.Fatalf("rows = %d, want 2", len(got.rows))
	}
	if got.rows[1].Depth != 1 {
		t.Errorf("reply depth = %d, want 1: a polled reply must nest under its parent", got.rows[1].Depth)
	}
}

func TestPollKeepsTheCursorOnTheSameMessage(t *testing.T) {
	m := openFolderModel(t)
	// Put the cursor on the older message, which stays second in the list.
	m.messageCursor = 1
	on := m.rows[1].Message.UID

	fresh := sampleMessages(3)[2]

	next, _ := m.Update(pollResultMsg(mail.PollResult{Messages: []mail.Message{fresh}, Total: 3}))
	got := next.(Model)

	if got.rows[got.messageCursor].Message.UID != on {
		t.Errorf("cursor moved to UID %d, want it left on %d",
			got.rows[got.messageCursor].Message.UID, on)
	}
}

func TestPollDroppingCountAsksForSurvivingUIDs(t *testing.T) {
	m := openFolderModel(t)

	var asked string
	m.countUIDs = func(acct config.Account, folder string) ([]imap.UID, error) {
		asked = folder

		return []imap.UID{2}, nil
	}

	next, cmd := m.Update(pollResultMsg(mail.PollResult{Total: 1}))
	if cmd == nil {
		t.Fatal("a poll whose count dropped did not ask for the surviving UIDs")
	}

	msg, ok := cmd().(uidsMsg)
	if !ok {
		t.Fatalf("the reconcile produced %T, want uidsMsg", cmd())
	}
	if asked != "INBOX" {
		t.Errorf("reconciled folder = %q, want INBOX", asked)
	}
	if len(msg.uids) != 1 || msg.uids[0] != 2 {
		t.Errorf("uids = %v, want just 2", msg.uids)
	}

	after, _ := next.(Model).Update(msg)
	got := after.(Model)
	if len(got.messages) != 1 || got.messages[0].UID != 2 {
		t.Errorf("messages = %v, want only UID 2", got.messages)
	}
}

func TestPollFailureLeavesTheListAndReports(t *testing.T) {
	m := openFolderModel(t)
	before := len(m.messages)

	next, cmd := m.Update(pollMsg{
		account: "fastmail",
		folder:  "INBOX",
		err:     errors.New("dial tcp: connection refused"),
	})

	got := next.(Model)
	if cmd != nil {
		t.Error("a failed poll asked for a reconcile")
	}
	if len(got.messages) != before {
		t.Errorf("messages = %d, want the %d already shown", len(got.messages), before)
	}
	if !strings.Contains(got.status, "connection refused") {
		t.Errorf("status = %q, want the failure", got.status)
	}
	if got.confirmOpen || got.accountsOpen {
		t.Error("a failed poll opened a dialog")
	}
}

func TestPollIgnoresAnotherFolderOrAccount(t *testing.T) {
	tests := []struct {
		name string
		msg  pollMsg
	}{
		{"another folder", pollMsg{account: "fastmail", folder: "Archive", result: mail.PollResult{Total: 9}}},
		{"another account", pollMsg{account: "work", folder: "INBOX", result: mail.PollResult{Total: 9}}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := openFolderModel(t)
			next, cmd := m.Update(tc.msg)

			got := next.(Model)
			if len(got.messages) != 2 {
				t.Errorf("messages = %d, want the 2 already shown", len(got.messages))
			}
			if cmd != nil {
				t.Error("a result for another folder or account asked for a reconcile")
			}
		})
	}
}

func TestStatusBarShowsTheOpenFoldersUnreadCount(t *testing.T) {
	m := openFolderModel(t)

	next, _ := m.Update(pollResultMsg(mail.PollResult{Total: 2, Unseen: 3}))
	m = next.(Model)

	if view := m.View(); !strings.Contains(view, "3 unread") {
		t.Errorf("status bar does not show the unread count:\n%s", view)
	}

	// Opening another folder must not leave the previous folder's number behind.
	m.folders = sampleFolders(2)
	m.folderCursor = 1
	next, _ = m.openSelectedFolder()
	if got := next.(Model).openUnseen; got != 0 {
		t.Errorf("openUnseen = %d after a folder change, want 0", got)
	}
}

func TestDeletedHighWaterMarkIsNotLowered(t *testing.T) {
	m := openFolderModel(t)
	m.messages = append(m.messages, sampleMessages(3)[2]) // UID 3
	m.lastSeen[folderKey{account: "fastmail", folder: "INBOX"}] = 3

	m.removeDeleted([]imap.UID{3})

	var gotSince imap.UID
	m.pollFolder = func(_ config.Account, _ string, since imap.UID) (mail.PollResult, error) {
		gotSince = since

		return mail.PollResult{}, nil
	}

	_, cmd := m.Update(pollTickMsg{})
	if cmd == nil {
		t.Fatal("a tick started no poll")
	}
	if _, ok := cmd().(pollMsg); !ok {
		t.Fatalf("the poll produced %T, want pollMsg", cmd())
	}

	if gotSince != 3 {
		t.Errorf("polled since = %d, want the deleted high-water mark 3", gotSince)
	}
}
