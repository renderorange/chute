package tui

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/emersion/go-imap/v2"

	"github.com/renderorange/chute/config"
	"github.com/renderorange/chute/mail"
)

// draftSavedMsg reports the outcome of an explicit draft save.
type draftSavedMsg struct {
	account string
	uid     imap.UID
	known   bool
	err     error

	previousUID   imap.UID
	previousKnown bool
	removeErr     error
}

// sendResultMsg reports what the three send steps managed to do. The fields are
// ordered, not independent: filed implies sent, draftRemoved implies filed.
type sendResultMsg struct {
	account      string
	sent         bool
	filed        bool
	draftRemoved bool
	draftKept    bool
	err          error
}

// composeLoadedMsg carries a draft fetched for editing.
type composeLoadedMsg struct {
	account string
	uid     imap.UID
	compose mail.Compose
	err     error
}

// accountFrom is the From value for this account: the configured one, or the login.
func (m Model) accountFrom() string {
	if len(m.cfg.Accounts) == 0 {
		return ""
	}

	acct := m.cfg.Accounts[m.account]
	if acct.From != "" {
		return acct.From
	}

	return acct.IMAP.Username
}

// beginCompose opens an empty composer for a new message.
func (m Model) beginCompose() (tea.Model, tea.Cmd) {
	from := m.accountFrom()
	if from == "" {
		m.status = "no From address: set [accounts] from or imap.username"
		return m, nil
	}

	m.compose = newComposer(mail.Compose{From: from, MessageID: mail.NewMessageID(from)}, false)
	m.composeDraftUID = 0
	m.composeDraftKnown = false
	m.status = ""

	return m, nil
}

// composeReady reports whether replying or forwarding makes sense. Unlike triage, which
// is list-only so a keypress in the folder panel cannot touch mail, replying from the
// reader is the normal path: the message is open in front of the user.
func (m Model) composeReady() bool {
	return m.focus == focusMessages || m.focus == focusReader
}

// beginReply opens a reply to the selected message. It needs the body, because the
// whole original is quoted, so a message that has not been opened says so instead of
// silently quoting nothing.
func (m Model) beginReply(all bool) (tea.Model, tea.Cmd) {
	if !m.composeReady() {
		return m, nil
	}

	msg, ok := m.selectedMessage()
	if !ok || m.openFolder == "" {
		m.status = "no message selected"
		return m, nil
	}

	key := bodyKey{account: m.accountName(), folder: m.openFolder, uid: msg.UID}
	body, cached := m.bodyFor(key)
	if !cached {
		m.status = "open the message first so the reply can quote it"
		return m, nil
	}

	from := m.accountFrom()
	if from == "" {
		m.status = "no From address: set [accounts] from or imap.username"
		return m, nil
	}

	// The body fetch asks for the whole message, so the original's own References chain
	// is available: it lists the ancestors above the parent. Fall back to the envelope's
	// In-Reply-To when the original carries no References header (old or non-conforming
	// mail); it names only the immediate ancestor, which is still better than nothing.
	// mail.ReplyCompose appends the parent's own id below, so it must not be added here.
	references := append([]string(nil), body.References...)
	if len(references) == 0 {
		references = append(references, msg.InReplyTo...)
	}

	orig := mail.Compose{
		From:       msg.FromAddress(),
		To:         msg.ToAddresses(),
		Cc:         msg.CcAddresses(),
		Subject:    msg.Subject(),
		Text:       body.Text,
		MessageID:  msg.MessageID,
		References: references,
	}

	bottom := m.cfg.General.ReplyAtBottom()
	reply := mail.ReplyCompose(orig, mail.ReplyOptions{
		From:        from,
		Self:        from,
		Body:        "",
		Attribution: mail.Attribution(msg.FromAddress(), msg.Date()),
		All:         all,
		Bottom:      bottom,
	})
	reply.MessageID = mail.NewMessageID(from)

	m.compose = newComposer(reply, bottom)
	m.composeDraftUID = 0
	m.composeDraftKnown = false
	m.status = ""

	return m, nil
}

// beginForward opens a forward of the selected message.
func (m Model) beginForward() (tea.Model, tea.Cmd) {
	if !m.composeReady() {
		return m, nil
	}

	msg, ok := m.selectedMessage()
	if !ok || m.openFolder == "" {
		m.status = "no message selected"
		return m, nil
	}

	key := bodyKey{account: m.accountName(), folder: m.openFolder, uid: msg.UID}
	body, cached := m.bodyFor(key)
	if !cached {
		m.status = "open the message first so the forward can include it"
		return m, nil
	}

	from := m.accountFrom()
	if from == "" {
		m.status = "no From address: set [accounts] from or imap.username"
		return m, nil
	}

	// The composer has no way to re-attach files, so say what is being left behind.
	// Silence here produces a forward that looks complete and is not (#61).
	notice := ""
	if n := len(body.Attachments); n > 0 {
		notice = "forwarding without " + countLabel(n, "attachment")
	}

	fwd := mail.ForwardCompose(mail.Compose{
		From:    msg.FromAddress(),
		To:      msg.ToAddresses(),
		Cc:      msg.CcAddresses(),
		Subject: msg.Subject(),
		Text:    body.Text,
	}, from, "")
	fwd.MessageID = mail.NewMessageID(from)

	m.compose = newComposer(fwd, false)
	m.composeDraftUID = 0
	m.composeDraftKnown = false
	m.status = notice

	return m, nil
}

// handleComposeKey gives the composer every keypress except the one that always quits.
func (m Model) handleComposeKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.String() == "ctrl+c" {
		return m, tea.Quit
	}

	switch m.compose.update(msg) {
	case composerClose:
		m.compose = nil
		m.status = ""

	case composerConfirmClose:
		m.confirmOpen = true
		m.confirmKind = confirmDiscardDraft

	case composerSave:
		return m, m.saveDraftCmd()

	case composerSend:
		return m.beginSend()
	}

	return m, nil
}

// beginSend starts the send once. The SMTP round trip and the two mailbox writes can
// take seconds, during which the composer stays on screen; without a status the app
// looks hung, and without the guard a second ctrl+d sends the message twice.
func (m Model) beginSend() (tea.Model, tea.Cmd) {
	if m.compose == nil || m.sending {
		return m, nil
	}

	cmd := m.sendComposeCmd()
	if cmd == nil {
		// Nothing to run, so nothing to report: setting the flag here would leave
		// the composer unable to send, waiting for a result that never comes.
		return m, nil
	}

	m.sending = true
	m.status = "sending..."

	return m, cmd
}

// messageFor builds the outgoing bytes for the composer's current contents.
func (m Model) messageFor() ([]byte, error) {
	c := m.compose.compose()
	c.From = m.accountFrom()
	if c.MessageID == "" {
		c.MessageID = mail.NewMessageID(c.From)
	}

	return mail.BuildMessage(c)
}

// saveDraftCmd appends the message to the account's drafts mailbox. A previous save of
// the same composer is removed afterwards, so saving twice does not leave a trail.
func (m Model) saveDraftCmd() tea.Cmd {
	if m.compose == nil {
		return nil
	}

	if len(m.cfg.Accounts) == 0 {
		return nil
	}

	acct := m.cfg.Accounts[m.account]

	folder, ok := m.draftsFolder(acct)
	if !ok || folder == "" {
		return func() tea.Msg {
			return draftSavedMsg{account: acct.Name, err: errNoDraftsFolder}
		}
	}

	msg, err := m.messageFor()
	if err != nil {
		return func() tea.Msg { return draftSavedMsg{account: acct.Name, err: err} }
	}

	save := m.saveDraft
	remove := m.deleteMessages
	previousUID, previousKnown := m.composeDraftUID, m.composeDraftKnown

	return func() tea.Msg {
		uid, known, err := save(acct, folder, msg)
		result := draftSavedMsg{
			account:       acct.Name,
			uid:           uid,
			known:         known,
			err:           err,
			previousUID:   previousUID,
			previousKnown: previousKnown,
		}
		if err != nil {
			return result
		}

		if previousKnown && remove != nil {
			result.removeErr = remove(acct, folder, []imap.UID{previousUID})
		}

		return result
	}
}

// errNoDraftsFolder is reported when the account has no drafts mailbox to write to.
var errNoDraftsFolder = fmt.Errorf("no drafts mailbox: set [accounts.imap] drafts")

// sendComposeCmd runs the three steps in order: SMTP, then the Sent copy, then the
// draft removal. Each step's failure leaves the earlier ones done, which is what lets
// the status line tell "went out but not filed" from "did not go out".
func (m Model) sendComposeCmd() tea.Cmd {
	if m.compose == nil || len(m.cfg.Accounts) == 0 {
		return nil
	}

	acct := m.cfg.Accounts[m.account]

	msg, err := m.messageFor()
	if err != nil {
		return func() tea.Msg { return sendResultMsg{account: acct.Name, err: err} }
	}

	send := m.sendMessage
	appendSent := m.appendSent
	remove := m.deleteMessages
	drafts, haveDrafts := m.draftsFolder(acct)
	draftUID, draftKnown := m.composeDraftUID, m.composeDraftKnown

	// The transmitted bytes carry Bcc so the envelope can address the blind recipients;
	// the filed copy must not disclose them (#62).
	filed := mail.StripBcc(msg)

	return func() tea.Msg {
		result := sendResultMsg{account: acct.Name}

		if send == nil {
			result.err = fmt.Errorf("no SMTP configuration for this account")

			return result
		}
		if err := send(acct, msg); err != nil {
			result.err = err

			return result
		}
		result.sent = true

		if appendSent != nil {
			if err := appendSent(acct, filed); err != nil {
				result.err = err

				return result
			}
		}
		result.filed = true

		if draftKnown && haveDrafts && remove != nil {
			if err := remove(acct, drafts, []imap.UID{draftUID}); err != nil {
				result.draftKept = true

				return result
			}
			result.draftRemoved = true
		}

		return result
	}
}

// openDraftCmd fetches a draft's fields so the composer can reopen it.
func (m Model) openDraftCmd(folder string, uid imap.UID) tea.Cmd {
	if m.fetchCompose == nil || len(m.cfg.Accounts) == 0 {
		return nil
	}

	acct := m.cfg.Accounts[m.account]
	fetch := m.fetchCompose

	return func() tea.Msg {
		compose, err := fetch(acct, folder, uid)

		var value mail.Compose
		if compose != nil {
			value = *compose
		}

		return composeLoadedMsg{account: acct.Name, uid: uid, compose: value, err: err}
	}
}

// draftsFolder resolves the account's drafts mailbox.
func (m Model) draftsFolder(acct config.Account) (string, bool) {
	if m.findDraftsFolder == nil {
		return "", false
	}

	return m.findDraftsFolder(acct)
}

// applyDraftSaved records the new draft's identity and reports the outcome.
func (m *Model) applyDraftSaved(msg draftSavedMsg) {
	if msg.account != m.accountName() {
		return
	}

	if msg.err != nil {
		m.status = "draft not saved: " + statusText(msg.err)
		return
	}

	m.composeDraftUID = msg.uid
	m.composeDraftKnown = msg.known

	switch {
	case !msg.known:
		m.status = "draft saved; this server cannot name it, so it cannot be removed after sending"
	case msg.removeErr != nil:
		m.status = "draft saved; the earlier copy could not be removed: " + statusText(msg.removeErr)
	default:
		m.status = "draft saved"
	}
}

// applySendResult reports the send outcome. The distinction that matters is between
// "went out" and "filed", because a user who cannot tell them apart sends twice.
func (m *Model) applySendResult(msg sendResultMsg) {
	// Clear the in-flight flag even for another account's result: otherwise a send
	// cut off by an account switch would leave the composer unable to send again.
	m.sending = false

	if msg.account != m.accountName() {
		return
	}

	switch {
	case !msg.sent:
		m.status = "send failed: " + statusText(msg.err)
	case !msg.filed:
		m.status = "message sent, but the copy was not filed in Sent: set [accounts.imap] sent"
	case msg.draftKept:
		m.status = "message sent; the draft is still in Drafts"
	default:
		m.compose = nil
		m.status = "message sent"
	}

	if msg.sent && !msg.filed && m.compose != nil {
		// Keep the text on screen: the draft in Drafts is the only record of it.
		m.status += " (draft kept)"
	}
}

// applyComposeLoaded reopens a draft in the composer.
func (m *Model) applyComposeLoaded(msg composeLoadedMsg) {
	if msg.account != m.accountName() {
		return
	}

	if msg.err != nil {
		m.status = "draft not opened: " + statusText(msg.err)
		return
	}

	if m.compose == nil {
		m.compose = newComposer(msg.compose, true)
	}
	m.compose = newComposer(msg.compose, true)
	m.composeDraftUID = msg.uid
	m.composeDraftKnown = true

	if msg.compose.HadAttachments {
		m.status = "draft opened; its attachments were dropped"
		return
	}

	m.status = ""
}

// renderComposer draws the composer over the whole content area.
func (m Model) renderComposer(layout Layout) string {
	box := Box{X: 0, Y: 0, Width: ContentWidth(m.width), Height: ContentHeight(m.height)}
	inner := box.Width - (2 * PanelPaddingHorizontal)
	if inner < 1 {
		inner = 1
	}

	body := m.compose.view(inner, max(1, box.Height-1))

	return m.renderPanel("compose", box, body, true)
}
