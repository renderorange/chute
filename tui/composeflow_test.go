package tui

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/emersion/go-imap/v2"

	"github.com/renderorange/chute/config"
	"github.com/renderorange/chute/mail"
)

// composeFakes records every step of the compose flow, in the order it happened, which
// is the property that keeps a failed Sent append from deleting the draft.
type composeFakes struct {
	order []string

	sendErr   error
	appendErr error
	deleteErr error

	savedFolder string
	savedMsg    []byte
	savedUID    imap.UID
	savedKnown  bool
	saveErr     error

	// sentMsg is what SMTP would have transmitted; appendedMsg is what was filed in
	// Sent. They differ only by the Bcc header (#62).
	sentMsg     []byte
	appendedMsg []byte

	removed []imap.UID
}

func (f *composeFakes) send(_ config.Account, msg []byte) error {
	f.order = append(f.order, "send")
	f.sentMsg = msg

	return f.sendErr
}

func (f *composeFakes) appendSent(_ config.Account, msg []byte) error {
	f.order = append(f.order, "append")
	f.appendedMsg = msg

	return f.appendErr
}

func (f *composeFakes) delete(_ config.Account, _ string, uids []imap.UID) error {
	f.order = append(f.order, "delete")
	f.removed = append(f.removed, uids...)

	return f.deleteErr
}

func (f *composeFakes) save(_ config.Account, folder string, msg []byte) (imap.UID, bool, error) {
	f.order = append(f.order, "save")
	f.savedFolder = folder
	f.savedMsg = msg

	return f.savedUID, f.savedKnown, f.saveErr
}

// withComposeModel returns an opened INBOX with one message's body cached, wired to the
// fakes, so a reply can quote it.
func withComposeModel(t *testing.T) (Model, *composeFakes) {
	t.Helper()

	return withComposeModelMessages(t, sampleMessages(2))
}

// withComposeModelMessages is withComposeModel over a supplied message list.
func withComposeModelMessages(t *testing.T, messages []mail.Message) (Model, *composeFakes) {
	t.Helper()

	fakes := &composeFakes{savedUID: 42, savedKnown: true}

	m := openInbox(t, withInbox(t, messages, nil))
	m.cfg.Accounts[m.account].From = "Me <me@example.org>"
	m.loadBody = func(config.Account, string, imap.UID) (*mail.Body, error) {
		return &mail.Body{Text: "original text"}, nil
	}
	m.sendMessage = fakes.send
	m.appendSent = fakes.appendSent
	m.deleteMessages = fakes.delete
	m.saveDraft = fakes.save
	m.findDraftsFolder = func(config.Account) (string, bool) { return "Drafts", true }

	// Open the first message so its body is cached for quoting.
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)
	if cmd != nil {
		next, _ = m.Update(cmd())
		m = next.(Model)
	}

	return m, fakes
}

// press sends one key to the model, runs the command it returns and delivers the result.
func press(t *testing.T, m Model, msg tea.KeyMsg) Model {
	t.Helper()

	next, cmd := m.Update(msg)
	m = next.(Model)

	if cmd != nil {
		next, _ = m.Update(cmd())
		m = next.(Model)
	}

	return m
}

// TestReplyOpensComposerWithQuotedBody proves the reply starts with the whole original
// quoted and the subject prefixed.
func TestReplyOpensComposerWithQuotedBody(t *testing.T) {
	m, _ := withComposeModel(t)

	m = press(t, m, keyMsg("r"))

	if m.compose == nil {
		t.Fatal("r opened no composer")
	}

	got := m.compose.compose()
	if !strings.Contains(got.Text, "> original text") {
		t.Errorf("body does not quote the original: %q", got.Text)
	}
	if !strings.HasPrefix(got.Subject, "Re: ") {
		t.Errorf("Subject = %q, want a Re: prefix", got.Subject)
	}
	if len(got.To) != 1 || !strings.Contains(got.To[0], "sender@example.org") {
		t.Errorf("To = %v, want the original sender", got.To)
	}
}

// TestReplyWithoutBodySaysSo proves a reply is not silently empty when the message has
// not been opened.
func TestReplyWithoutBodySaysSo(t *testing.T) {
	m := openInbox(t, withInbox(t, sampleMessages(2), nil))

	m = press(t, m, keyMsg("r"))

	if m.compose != nil {
		t.Fatal("reply opened without a body to quote")
	}
	if !strings.Contains(m.status, "open the message") {
		t.Errorf("status = %q", m.status)
	}
}

// TestSendRunsTheThreeStepsInOrder is the core assertion: SMTP, then the Sent copy,
// then the draft removal.
func TestSendRunsTheThreeStepsInOrder(t *testing.T) {
	m, fakes := withComposeModel(t)
	m = press(t, m, keyMsg("r"))

	m.composeDraftUID = 7
	m.composeDraftKnown = true

	m = press(t, m, namedKey("ctrl+d"))

	want := []string{"send", "append", "delete"}
	if len(fakes.order) != len(want) {
		t.Fatalf("steps = %v, want %v", fakes.order, want)
	}
	for i := range want {
		if fakes.order[i] != want[i] {
			t.Fatalf("steps = %v, want %v", fakes.order, want)
		}
	}
	if m.compose != nil {
		t.Error("the composer stayed open after a successful send")
	}
	if m.status != "message sent" {
		t.Errorf("status = %q", m.status)
	}
}

// TestSendKeepsTheDraftWhenTheSentAppendFails is the partial failure the design calls
// out: the mail went out, the copy was not filed, and the text must not be lost.
func TestSendKeepsTheDraftWhenTheSentAppendFails(t *testing.T) {
	m, fakes := withComposeModel(t)
	fakes.appendErr = errors.New("no such mailbox")
	m = press(t, m, keyMsg("r"))

	m.composeDraftUID = 7
	m.composeDraftKnown = true

	m = press(t, m, namedKey("ctrl+d"))

	if len(fakes.order) != 2 || fakes.order[1] != "append" {
		t.Fatalf("steps = %v, want send then append", fakes.order)
	}
	if len(fakes.removed) != 0 {
		t.Errorf("the draft was removed after a failed Sent append: %v", fakes.removed)
	}
	if m.compose == nil {
		t.Error("the composer closed, losing the only remaining record of the message")
	}
	if !strings.Contains(m.status, "not filed") {
		t.Errorf("status = %q, want it to say the copy was not filed", m.status)
	}
}

// TestSendFailureStopsBeforeFiling proves a failed SMTP send does not touch Sent or the
// draft.
func TestSendFailureStopsBeforeFiling(t *testing.T) {
	m, fakes := withComposeModel(t)
	fakes.sendErr = errors.New("connection refused")
	m = press(t, m, keyMsg("r"))

	m.composeDraftUID = 7
	m.composeDraftKnown = true

	m = press(t, m, namedKey("ctrl+d"))

	if len(fakes.order) != 1 || fakes.order[0] != "send" {
		t.Fatalf("steps = %v, want only the send attempt", fakes.order)
	}
	if len(fakes.removed) != 0 {
		t.Errorf("the draft was removed after a failed send: %v", fakes.removed)
	}
	if m.compose == nil {
		t.Error("the composer closed on a failed send")
	}
	if !strings.Contains(m.status, "send failed") {
		t.Errorf("status = %q", m.status)
	}
}

// TestSaveDraftUsesTheDraftsFolder proves ctrl+s appends to the resolved mailbox and
// reports it.
func TestSaveDraftUsesTheDraftsFolder(t *testing.T) {
	m, fakes := withComposeModel(t)
	m = press(t, m, keyMsg("r"))

	m = press(t, m, namedKey("ctrl+s"))

	if fakes.savedFolder != "Drafts" {
		t.Errorf("saved to %q, want Drafts", fakes.savedFolder)
	}
	if len(fakes.savedMsg) == 0 {
		t.Error("no message bytes were saved")
	}
	if m.composeDraftUID != fakes.savedUID || !m.composeDraftKnown {
		t.Errorf("draft identity = %d/%v, want %d/true", m.composeDraftUID, m.composeDraftKnown, fakes.savedUID)
	}
	if m.status != "draft saved" {
		t.Errorf("status = %q", m.status)
	}
}

// TestSaveDraftReportsMissingFolder proves a missing drafts mailbox is reported and the
// text stays in the composer.
func TestSaveDraftReportsMissingFolder(t *testing.T) {
	m, fakes := withComposeModel(t)
	m.findDraftsFolder = func(config.Account) (string, bool) { return "", false }
	m = press(t, m, keyMsg("r"))

	m = press(t, m, namedKey("ctrl+s"))

	if len(fakes.order) != 0 {
		t.Errorf("the save reached the server: %v", fakes.order)
	}
	if m.compose == nil {
		t.Error("the composer closed instead of keeping the text")
	}
	if !strings.Contains(m.status, "drafts") {
		t.Errorf("status = %q, want it to name the drafts mailbox", m.status)
	}
}

// TestDiscardNeedsConfirmation proves esc on a dirty composer asks, and that cancelling
// keeps the text.
func TestDiscardNeedsConfirmation(t *testing.T) {
	m, _ := withComposeModel(t)
	m = press(t, m, keyMsg("r"))
	m = press(t, m, key('x'))

	m = press(t, m, namedKey("esc"))
	if !m.confirmOpen || m.confirmKind != confirmDiscardDraft {
		t.Fatalf("esc did not open a discard confirmation: open=%v kind=%v", m.confirmOpen, m.confirmKind)
	}

	m = press(t, m, namedKey("esc"))
	if m.compose == nil {
		t.Fatal("cancelling the confirmation discarded the message")
	}
	if m.status != "kept editing" {
		t.Errorf("status = %q", m.status)
	}

	m = press(t, m, namedKey("esc"))
	m = press(t, m, namedKey("enter"))
	if m.compose != nil {
		t.Error("confirming the discard left the composer open")
	}
	if m.status != "message discarded" {
		t.Errorf("status = %q", m.status)
	}
}

// TestComposerSwallowsGlobalKeys proves q types instead of quitting, while ctrl+c still
// quits: losing a paragraph to a stray key is the worst outcome here.
func TestComposerSwallowsGlobalKeys(t *testing.T) {
	m, _ := withComposeModel(t)
	m = press(t, m, keyMsg("r"))

	next, cmd := m.Update(key('q'))
	m = next.(Model)

	if cmd != nil {
		t.Errorf("q in the composer returned a command (status=%q focus=%v compose=%v)", m.status, m.focus, m.compose != nil)
	}
	if m.compose == nil {
		t.Fatalf("the composer closed on q; status=%q focus=%v", m.status, m.focus)
	}
	if !strings.Contains(m.compose.to.text(), "q") {
		t.Errorf("q did not reach the field: %q", m.compose.to.text())
	}

	_, cmd = m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if cmd == nil {
		t.Fatal("ctrl+c did not quit from the composer")
	}
}

// TestOpeningADraftLoadsItIntoTheComposer proves the Drafts mailbox is an editor, not a
// reader.
func TestOpeningADraftLoadsItIntoTheComposer(t *testing.T) {
	m := openInbox(t, withInbox(t, sampleMessages(2), nil))
	m.openFolder = "Drafts"
	m.findDraftsFolder = func(config.Account) (string, bool) { return "Drafts", true }
	m.fetchCompose = func(config.Account, string, imap.UID) (*mail.Compose, error) {
		return &mail.Compose{
			From:      "me@example.org",
			To:        []string{"one@example.com"},
			Subject:   "draft subject",
			Text:      "draft text",
			MessageID: "<draft@example.org>",
		}, nil
	}

	m = press(t, m, tea.KeyMsg{Type: tea.KeyEnter})

	if m.compose == nil {
		t.Fatal("opening a draft did not open the composer")
	}
	if got := m.compose.compose().Subject; got != "draft subject" {
		t.Errorf("Subject = %q", got)
	}
	if !m.composeDraftKnown || m.composeDraftUID == 0 {
		t.Errorf("draft identity = %d/%v", m.composeDraftUID, m.composeDraftKnown)
	}
	if got := m.compose.compose().MessageID; got != "<draft@example.org>" {
		t.Errorf("MessageID = %q, want the draft's own id", got)
	}
}

// TestReplyAllFillsCc proves R reaches the reply-all builder.
func TestReplyAllFillsCc(t *testing.T) {
	messages := sampleMessages(2)
	// The list sorts by date descending, so the cursor starts on the newest message.
	messages[1].Envelope.Cc = []imap.Address{{Name: "Other", Mailbox: "other", Host: "example.org"}}

	m, _ := withComposeModelMessages(t, messages)

	m = press(t, m, keyMsg("R"))

	if m.compose == nil {
		t.Fatal("R opened no composer")
	}
	if got := m.compose.compose().Cc; len(got) != 1 || !strings.Contains(got[0], "other@example.org") {
		t.Errorf("Cc = %v", got)
	}
}

// TestComposeKeyOpensEmptyComposer covers c: a new message starts with no recipients.
func TestComposeKeyOpensEmptyComposer(t *testing.T) {
	m, _ := withComposeModel(t)

	m = press(t, m, keyMsg("c"))

	if m.compose == nil {
		t.Fatal("c opened no composer")
	}

	got := m.compose.compose()
	if len(got.To) != 0 || len(got.Cc) != 0 {
		t.Errorf("a new message pre-fills recipients: %v %v", got.To, got.Cc)
	}
	if got.MessageID == "" {
		t.Error("a new message has no Message-ID")
	}
	if got.MessageID == "" {
		t.Error("the composer lost its Message-ID")
	}
	if again := m.compose.compose().MessageID; again != got.MessageID {
		t.Errorf("Message-ID changed between reads: %q then %q", got.MessageID, again)
	}
}

// TestForwardKeySeparatesWithoutQuoting covers F: the original is included, unprefixed.
func TestForwardKeySeparatesWithoutQuoting(t *testing.T) {
	m, _ := withComposeModel(t)

	m = press(t, m, keyMsg("F"))

	if m.compose == nil {
		t.Fatal("F opened no composer")
	}

	got := m.compose.compose()
	if len(got.To) != 0 {
		t.Errorf("a forward pre-fills recipients: %v", got.To)
	}
	if !strings.Contains(got.Text, "---------- Forwarded message ----------") {
		t.Errorf("the forward has no separator: %q", got.Text)
	}
	if strings.Contains(got.Text, "> original text") {
		t.Errorf("the forward quoted the original: %q", got.Text)
	}
}

// TestForwardWarnsAboutDroppedAttachments is #61: the composer cannot re-attach files, so
// a forward must say that the attachments are being left behind instead of silently
// producing a message without them.
func TestForwardWarnsAboutDroppedAttachments(t *testing.T) {
	m, _ := withComposeModel(t)

	m.bodies[m.selectedBodyKey()] = &mail.Body{
		Text: "original text",
		Attachments: []mail.Attachment{
			{Filename: "invoice.pdf", ContentType: "application/pdf"},
			{Filename: "photo.jpg", ContentType: "image/jpeg"},
		},
	}

	m = press(t, m, keyMsg("F"))

	if m.compose == nil {
		t.Fatal("F opened no composer")
	}
	if want := "forwarding without 2 attachments"; m.status != want {
		t.Errorf("status = %q, want %q", m.status, want)
	}
}

// TestForwardWithoutAttachmentsDoesNotWarn keeps the notice specific to the case it
// describes; an attachment-free forward must not mention attachments.
func TestForwardWithoutAttachmentsDoesNotWarn(t *testing.T) {
	m, _ := withComposeModel(t)

	m = press(t, m, keyMsg("F"))

	if m.compose == nil {
		t.Fatal("F opened no composer")
	}
	if strings.Contains(m.status, "attachment") {
		t.Errorf("status = %q, want no attachment notice", m.status)
	}
}

// TestSecondSaveRemovesTheEarlierDraft proves saving twice does not leave a trail: the
// earlier copy is removed once the new one lands.
func TestSecondSaveRemovesTheEarlierDraft(t *testing.T) {
	m, fakes := withComposeModel(t)
	m = press(t, m, keyMsg("r"))

	m = press(t, m, namedKey("ctrl+s"))
	firstUID := m.composeDraftUID

	m = press(t, m, namedKey("ctrl+s"))

	if len(fakes.removed) != 1 || fakes.removed[0] != firstUID {
		t.Errorf("removed %v, want the first draft %d", fakes.removed, firstUID)
	}
	if m.status != "draft saved" {
		t.Errorf("status = %q", m.status)
	}
}

// TestSendReportsADraftThatSurvives proves the third step's failure is reported without
// pretending the send failed.
func TestSendReportsADraftThatSurvives(t *testing.T) {
	m, fakes := withComposeModel(t)
	fakes.deleteErr = errors.New("trash unavailable")
	m = press(t, m, keyMsg("r"))

	m.composeDraftUID = 7
	m.composeDraftKnown = true

	m = press(t, m, namedKey("ctrl+d"))

	if m.status != "message sent; the draft is still in Drafts" {
		t.Errorf("status = %q", m.status)
	}
}

// TestViewDrawsTheComposer covers the render path: the panel is titled, the hints are
// the composer's, and every line is exactly the terminal width.
func TestViewDrawsTheComposer(t *testing.T) {
	m, _ := withComposeModel(t)
	m = press(t, m, keyMsg("r"))

	view := m.View()

	if !strings.Contains(view, "compose") {
		t.Errorf("the view has no compose panel:\n%s", view)
	}
	for _, want := range []string{"To:", "ctrl+s:save"} {
		if !strings.Contains(view, want) {
			t.Errorf("the view is missing %q:\n%s", want, view)
		}
	}

	for i, line := range strings.Split(view, "\n") {
		if got := len([]rune(line)); got != m.width {
			t.Fatalf("line %d is %d cells wide, want %d", i, got, m.width)
		}
	}
}

// TestDraftWithAttachmentsSaysItDroppedThem proves the limitation is reported instead of
// silently losing the attachment.
func TestDraftWithAttachmentsSaysItDroppedThem(t *testing.T) {
	m := openInbox(t, withInbox(t, sampleMessages(1), nil))
	m.openFolder = "Drafts"
	m.findDraftsFolder = func(config.Account) (string, bool) { return "Drafts", true }
	m.fetchCompose = func(config.Account, string, imap.UID) (*mail.Compose, error) {
		return &mail.Compose{Subject: "with attachment", HadAttachments: true}, nil
	}

	m = press(t, m, tea.KeyMsg{Type: tea.KeyEnter})

	if !strings.Contains(m.status, "attachments were dropped") {
		t.Errorf("status = %q", m.status)
	}
}

// TestSaveDraftReportsAnUnnameableDraft proves the composer warns when the server cannot
// name the draft, because that draft can never be removed automatically.
func TestSaveDraftReportsAnUnnameableDraft(t *testing.T) {
	m, fakes := withComposeModel(t)
	fakes.savedKnown = false
	m = press(t, m, keyMsg("r"))
	m = press(t, m, namedKey("ctrl+s"))

	if !strings.Contains(m.status, "cannot name it") {
		t.Errorf("status = %q", m.status)
	}
	if m.composeDraftKnown {
		t.Error("the composer recorded a known draft identity the server never gave")
	}
}

// TestSaveDraftReportsAFailedRemoval proves a leftover earlier copy is reported.
func TestSaveDraftReportsAFailedRemoval(t *testing.T) {
	m, fakes := withComposeModel(t)
	fakes.deleteErr = errors.New("trash unavailable")
	m = press(t, m, keyMsg("r"))

	m = press(t, m, namedKey("ctrl+s"))
	m = press(t, m, namedKey("ctrl+s"))

	if !strings.Contains(m.status, "earlier copy could not be removed") {
		t.Errorf("status = %q", m.status)
	}
}

// TestComposeWithoutFromSaysSo covers the misconfigured account: no From anywhere means
// nothing can be sent, and the status has to say how to fix it.
func TestComposeWithoutFromSaysSo(t *testing.T) {
	m := openInbox(t, withInbox(t, sampleMessages(1), nil))
	m.cfg.Accounts[m.account].From = ""
	m.cfg.Accounts[m.account].IMAP.Username = ""

	m = press(t, m, keyMsg("c"))

	if m.compose != nil {
		t.Fatal("a composer opened with no From address")
	}
	if !strings.Contains(m.status, "From address") {
		t.Errorf("status = %q", m.status)
	}
}

// TestForwardWithoutBodySaysSo proves a forward is not silently empty.
func TestForwardWithoutBodySaysSo(t *testing.T) {
	m := openInbox(t, withInbox(t, sampleMessages(1), nil))
	m.cfg.Accounts[m.account].From = "me@example.org"

	m = press(t, m, keyMsg("F"))

	if m.compose != nil {
		t.Fatal("a forward opened without the original body")
	}
	if !strings.Contains(m.status, "open the message") {
		t.Errorf("status = %q", m.status)
	}
}

// TestReplyAndForwardNeedTheListOrReader proves r, R and F do nothing while the folders
// panel has focus: starting a message from the folder list is how a mis-aimed keypress
// would otherwise reach the server.
func TestReplyAndForwardNeedTheListOrReader(t *testing.T) {
	for _, k := range []string{"r", "R", "F"} {
		m, fakes := withComposeModel(t)
		m.focus = focusFolders

		m = press(t, m, keyMsg(k))

		if m.compose != nil {
			t.Fatalf("%s opened a composer from the folders panel", k)
		}
		if len(fakes.order) != 0 {
			t.Errorf("%s reached the server: %v", k, fakes.order)
		}
		if m.status != "" {
			t.Errorf("%s set the status to %q, want silence", k, m.status)
		}
	}
}

// TestReplyWithoutAFromAddressSaysSo proves a reply from an account with no From is
// refused at the point of opening, with the fix, rather than failing at send time.
func TestReplyWithoutAFromAddressSaysSo(t *testing.T) {
	m, _ := withComposeModel(t)
	m.cfg.Accounts[m.account].From = ""
	m.cfg.Accounts[m.account].IMAP.Username = ""

	m = press(t, m, keyMsg("r"))

	if m.compose != nil {
		t.Fatal("a reply opened with no From address")
	}
	if !strings.Contains(m.status, "From address") {
		t.Errorf("status = %q", m.status)
	}
}

// TestForwardWithoutAFromAddressSaysSo is the same guard on the forward path.
func TestForwardWithoutAFromAddressSaysSo(t *testing.T) {
	m, _ := withComposeModel(t)
	m.cfg.Accounts[m.account].From = ""
	m.cfg.Accounts[m.account].IMAP.Username = ""

	m = press(t, m, keyMsg("F"))

	if m.compose != nil {
		t.Fatal("a forward opened with no From address")
	}
	if !strings.Contains(m.status, "From address") {
		t.Errorf("status = %q", m.status)
	}
}

// TestForwardWithoutASelectionSaysSo proves F with no open folder reports that there is
// nothing to forward instead of opening an empty one.
func TestForwardWithoutASelectionSaysSo(t *testing.T) {
	m, _ := withComposeModel(t)
	m.openFolder = ""

	m = press(t, m, keyMsg("F"))

	if m.compose != nil {
		t.Fatal("a forward opened with no folder open")
	}
	if !strings.Contains(m.status, "no message selected") {
		t.Errorf("status = %q", m.status)
	}
}

// TestEscapeClosesACleanComposer proves esc leaves the composer immediately when nothing
// was edited: only a dirty composer asks, because only a dirty one has text to lose.
func TestEscapeClosesACleanComposer(t *testing.T) {
	m, _ := withComposeModel(t)
	m = press(t, m, keyMsg("r"))
	m.status = "something else"

	m = press(t, m, namedKey("esc"))

	if m.compose != nil {
		t.Error("esc left a clean composer open")
	}
	if m.status != "" {
		t.Errorf("status = %q, want the closed composer to clear it", m.status)
	}
	if m.confirmOpen {
		t.Error("esc on a clean composer asked for a confirmation")
	}
}

// TestReplyWithoutASelectionSaysSo proves r with no folder open reports that there is
// nothing to reply to instead of opening a composer with no recipient.
func TestReplyWithoutASelectionSaysSo(t *testing.T) {
	m, _ := withComposeModel(t)
	m.openFolder = ""

	m = press(t, m, keyMsg("r"))

	if m.compose != nil {
		t.Fatal("a reply opened with no folder open")
	}
	if !strings.Contains(m.status, "no message selected") {
		t.Errorf("status = %q", m.status)
	}
}

// TestMessageForAlwaysCarriesAnID proves a message built from a composer without an id
// still gets one, so no outgoing message reaches SMTP without a Message-ID.
func TestMessageForAlwaysCarriesAnID(t *testing.T) {
	m, _ := withComposeModel(t)
	m.compose = newComposer(mail.Compose{
		From: "me@example.org",
		To:   []string{"you@example.org"},
	}, false)

	raw, err := m.messageFor()
	if err != nil {
		t.Fatalf("messageFor: %v", err)
	}
	if !strings.Contains(string(raw), "Message-Id: <") {
		t.Errorf("the built message has no Message-ID:\n%s", raw)
	}
}

// threadedModel opens a single message that already sits in a thread: it has its own
// Message-ID and a parent recorded in In-Reply-To. The list reads the Message fields,
// which mail.Client copies out of the envelope when it fetches (mail/client.go:380-384).
func threadedModel(t *testing.T) Model {
	t.Helper()

	messages := sampleMessages(1)
	messages[0].MessageID = "<parent@example.org>"
	messages[0].InReplyTo = []string{"<grandparent@example.org>"}

	m, _ := withComposeModelMessages(t, messages)

	return m
}

// TestReplyCarriesThreadingHeaders proves a reply names its parent in In-Reply-To and
// repeats the ancestor chain plus the parent in References. Without both, a recipient's
// client files the reply beside the original instead of under it.
func TestReplyCarriesThreadingHeaders(t *testing.T) {
	m := threadedModel(t)
	m = press(t, m, keyMsg("r"))

	if m.compose == nil {
		t.Fatal("r opened no composer")
	}

	raw, err := m.messageFor()
	if err != nil {
		t.Fatalf("messageFor: %v", err)
	}

	for _, want := range []string{
		"In-Reply-To: <parent@example.org>",
		"References: <grandparent@example.org> <parent@example.org>",
	} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("the built reply is missing %q:\n%s", want, raw)
		}
	}
}

// TestReplyAllCarriesThreadingHeaders is the same claim on the reply-all path.
func TestReplyAllCarriesThreadingHeaders(t *testing.T) {
	m := threadedModel(t)
	m = press(t, m, keyMsg("R"))

	if m.compose == nil {
		t.Fatal("R opened no composer")
	}

	raw, err := m.messageFor()
	if err != nil {
		t.Fatalf("messageFor: %v", err)
	}

	for _, want := range []string{
		"In-Reply-To: <parent@example.org>",
		"References: <grandparent@example.org> <parent@example.org>",
	} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("the built reply-all is missing %q:\n%s", want, raw)
		}
	}
}

// TestReplyToARootHasNoThreadingHeaders proves a message with no Message-ID produces a
// reply with no threading headers rather than headers pointing at nothing.
func TestReplyToARootHasNoThreadingHeaders(t *testing.T) {
	m, _ := withComposeModel(t)
	m = press(t, m, keyMsg("r"))

	raw, err := m.messageFor()
	if err != nil {
		t.Fatalf("messageFor: %v", err)
	}

	for _, unwanted := range []string{"In-Reply-To:", "References:"} {
		if strings.Contains(string(raw), unwanted) {
			t.Errorf("a reply to a root message carries %q:\n%s", unwanted, raw)
		}
	}
}

// TestForwardCarriesNoThreadingHeaders proves F does not file the forward inside the
// original's thread, which is what mail.ForwardCompose already decides.
func TestForwardCarriesNoThreadingHeaders(t *testing.T) {
	m := threadedModel(t)
	m = press(t, m, keyMsg("F"))

	if m.compose == nil {
		t.Fatal("F opened no composer")
	}

	raw, err := m.messageFor()
	if err != nil {
		t.Fatalf("messageFor: %v", err)
	}

	for _, unwanted := range []string{"In-Reply-To:", "References:"} {
		if strings.Contains(string(raw), unwanted) {
			t.Errorf("the forward carries %q:\n%s", unwanted, raw)
		}
	}
}

// TestReopenedDraftKeepsThreadingHeaders proves the draft round trip is lossless: a draft
// fetched back into the composer still builds both threading headers, so sending a drafted
// reply lands it under the original rather than beside it.
func TestReopenedDraftKeepsThreadingHeaders(t *testing.T) {
	m := openInbox(t, withInbox(t, sampleMessages(1), nil))
	m.cfg.Accounts[m.account].From = "me@example.org"
	m.openFolder = "Drafts"
	m.findDraftsFolder = func(config.Account) (string, bool) { return "Drafts", true }
	m.fetchCompose = func(config.Account, string, imap.UID) (*mail.Compose, error) {
		return &mail.Compose{
			From:       "me@example.org",
			To:         []string{"one@example.com"},
			Subject:    "Re: subject",
			Text:       "draft text",
			MessageID:  "draft@example.org",
			InReplyTo:  "parent@example.org",
			References: []string{"grandparent@example.org", "parent@example.org"},
		}, nil
	}

	m = press(t, m, tea.KeyMsg{Type: tea.KeyEnter})

	if m.compose == nil {
		t.Fatal("opening the draft did not open the composer")
	}

	raw, err := m.messageFor()
	if err != nil {
		t.Fatalf("messageFor: %v", err)
	}

	for _, want := range []string{
		"In-Reply-To: <parent@example.org>",
		"References: <grandparent@example.org> <parent@example.org>",
	} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("the reopened draft is missing %q:\n%s", want, raw)
		}
	}
}

// TestComposeResultsForAnotherAccountAreIgnored proves a save, send or draft fetch that
// finishes after the user switches accounts cannot rewrite the new account's status or
// replace the message being edited.
func TestComposeResultsForAnotherAccountAreIgnored(t *testing.T) {
	m, _ := withComposeModel(t)
	m = press(t, m, keyMsg("r"))
	m.status = "untouched"
	editing := m.compose

	m.applyDraftSaved(draftSavedMsg{account: "other", err: errors.New("save failed")})
	m.applySendResult(sendResultMsg{account: "other", err: errors.New("send failed")})
	m.applyComposeLoaded(composeLoadedMsg{account: "other", compose: mail.Compose{Subject: "someone else's draft"}})

	if m.status != "untouched" {
		t.Errorf("status = %q, want it untouched by another account's result", m.status)
	}
	if m.compose != editing {
		t.Error("another account's draft replaced the message being edited")
	}
}

// TestOpeningADraftReportsAFailedFetch proves a draft that cannot be fetched says so and
// leaves the list alone, instead of opening an empty composer.
func TestOpeningADraftReportsAFailedFetch(t *testing.T) {
	m := openInbox(t, withInbox(t, sampleMessages(1), nil))
	m.openFolder = "Drafts"
	m.findDraftsFolder = func(config.Account) (string, bool) { return "Drafts", true }
	m.fetchCompose = func(config.Account, string, imap.UID) (*mail.Compose, error) {
		return nil, errors.New("no such message")
	}

	m = press(t, m, tea.KeyMsg{Type: tea.KeyEnter})

	if m.compose != nil {
		t.Error("a failed draft fetch opened a composer")
	}
	if !strings.Contains(m.status, "draft not opened") {
		t.Errorf("status = %q", m.status)
	}
}

// TestSendWithoutSMTPIsReported proves an account with no send configured fails loudly
// and keeps the draft, rather than appearing to have sent.
func TestSendWithoutSMTPIsReported(t *testing.T) {
	m, fakes := withComposeModel(t)
	m.sendMessage = nil
	m = press(t, m, keyMsg("r"))

	m.composeDraftUID = 7
	m.composeDraftKnown = true

	m = press(t, m, namedKey("ctrl+d"))

	if m.compose == nil {
		t.Error("the composer closed on a send that never happened")
	}
	if len(fakes.order) != 0 {
		t.Errorf("steps = %v, want none", fakes.order)
	}
	if !strings.Contains(m.status, "no SMTP") {
		t.Errorf("status = %q", m.status)
	}
}
