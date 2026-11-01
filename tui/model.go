package tui

import (
	"errors"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/emersion/go-imap/v2"
	"github.com/renderorange/chute/config"
	"github.com/renderorange/chute/mail"
)

type panelFocus int

const (
	focusFolders panelFocus = iota
	focusMessages
	focusReader
)

// connState is the per-account connection status shown in the status bar.
type connState int

const (
	stateConnecting connState = iota
	stateConnected
	stateFailed
)

// Model is the root bubbletea model.
type Model struct {
	version string
	cfg     *config.Config
	account int

	sessions *mail.Manager

	width  int
	height int

	focus   panelFocus
	state   connState
	status  string
	started bool

	folders      []mail.Folder
	folderCursor int
	folderScroll int

	openFolder    string
	messages      []mail.Message
	rows          []Row
	messageCursor int
	messageScroll int
	msgLoading    bool

	bodies       map[bodyKey]*mail.Body
	loadingBody  bodyKey
	readerScroll int

	visualAnchor   int
	markSeenOnLoad map[bodyKey]bool

	attachOpen   bool
	attachCursor int

	foldersOpen         bool
	folderOverlayCursor int

	confirmOpen   bool
	confirmKind   confirmKind
	pendingDelete []imap.UID

	compose           *composer
	composeDraftUID   imap.UID
	composeDraftKnown bool

	loadFolders      loadFoldersFunc
	loadMessages     loadMessagesFunc
	loadBody         loadBodyFunc
	storeFlags       storeFlagsFunc
	deleteMessages   deleteMessagesFunc
	fetchCompose     fetchComposeFunc
	saveDraft        saveDraftFunc
	appendSent       appendSentFunc
	sendMessage      sendMessageFunc
	findDraftsFolder draftsFolderFunc

	keys bindings
}

// New builds the root model for a loaded config. mailOptions apply to every account
// connection the model opens, which is where a debug writer enters the TUI path.
func New(cfg *config.Config, version string, mailOptions ...mail.Option) Model {
	sessions := mail.NewManager(mailOptions...)
	load := sessionLoaders(sessions)

	m := Model{
		version:          version,
		cfg:              cfg,
		state:            stateConnecting,
		sessions:         sessions,
		loadFolders:      load.folders,
		loadMessages:     load.messages,
		loadBody:         load.body,
		storeFlags:       load.storeFlags,
		deleteMessages:   load.deleteMessages,
		fetchCompose:     load.fetchCompose,
		saveDraft:        load.saveDraft,
		appendSent:       load.appendSent,
		sendMessage:      load.sendMessage,
		findDraftsFolder: load.findDraftsFolder,
		bodies:           make(map[bodyKey]*mail.Body),
		markSeenOnLoad:   make(map[bodyKey]bool),
		visualAnchor:     noVisualAnchor,
		keys:             defaultBindings(),
	}

	for i := range cfg.Accounts {
		if cfg.Accounts[i].Default {
			m.account = i
			break
		}
	}

	return m
}

// Close logs out every persistent account connection.
func (m Model) Close() error {
	if m.sessions == nil {
		return nil
	}

	return m.sessions.Close()
}

// Init starts the first folder load.
func (m Model) Init() tea.Cmd {
	return m.reloadCmd()
}

// reloadCmd loads folders for the active account in the background.
func (m Model) reloadCmd() tea.Cmd {
	if len(m.cfg.Accounts) == 0 {
		return nil
	}

	acct := m.cfg.Accounts[m.account]
	load := m.loadFolders
	if load == nil {
		return nil
	}

	return func() tea.Msg {
		folders, err := load(acct)
		return foldersMsg{account: acct.Name, folders: folders, err: err}
	}
}

// loadMessagesCmd loads one folder's envelopes in the background.
func (m Model) loadMessagesCmd(folder string) tea.Cmd {
	if len(m.cfg.Accounts) == 0 {
		return nil
	}

	acct := m.cfg.Accounts[m.account]
	load := m.loadMessages
	if load == nil {
		return nil
	}

	return func() tea.Msg {
		messages, err := load(acct, folder)
		return messagesMsg{account: acct.Name, folder: folder, messages: messages, err: err}
	}
}

// Update handles terminal events and loader results.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case draftSavedMsg:
		m.applyDraftSaved(msg)

		return m, nil

	case sendResultMsg:
		m.applySendResult(msg)

		return m, nil

	case composeLoadedMsg:
		m.applyComposeLoaded(msg)

		return m, nil

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.clampFocus()
		m.clampFolderScroll()
		m.clampMessageScroll()
		m.clampReaderScroll()

		return m, nil

	case foldersMsg:
		if msg.account != m.accountName() {
			return m, nil
		}

		if msg.err != nil {
			m.state = stateFailed
			m.status = msg.err.Error()
			m.folders = nil
			m.folderCursor = 0
			m.folderScroll = 0

			return m, nil
		}

		m.state = stateConnected
		m.status = ""
		m.folders = msg.folders
		m.folderCursor = 0
		m.folderScroll = 0
		m.foldersOpen = false
		m.folderOverlayCursor = 0
		m.clampFolderScroll()

		if !m.started {
			m.started = true
			if folder, ok := m.startFolder(); ok {
				for i := range m.folders {
					if m.folders[i].Name == folder.Name {
						m.folderCursor = i
						break
					}
				}
				return m.openSelectedFolder()
			}
			if name := m.cfg.Accounts[m.account].StartFolderName(); name != "" {
				m.status = fmt.Sprintf("start folder %q not found", name)
			}
		}

		return m, nil

	case messagesMsg:
		if msg.account != m.accountName() || msg.folder != m.openFolder {
			return m, nil
		}

		m.msgLoading = false

		if msg.err != nil {
			m.status = msg.err.Error()
			m.messages = nil
			m.rows = nil

			return m, nil
		}

		m.messages = sortNewestFirst(msg.messages)
		m.rows = BuildRows(m.messages)
		m.messageCursor = 0
		m.messageScroll = 0
		m.visualAnchor = noVisualAnchor
		m.clampMessageScroll()

		return m, nil

	case bodyMsg:
		if msg.account != m.accountName() {
			return m, nil
		}

		key := bodyKey{account: msg.account, folder: msg.folder, uid: msg.uid}
		if m.loadingBody == key {
			m.loadingBody = bodyKey{}
		}

		if msg.err != nil {
			if m.markSeenOnLoad != nil {
				delete(m.markSeenOnLoad, key)
			}
			m.status = msg.err.Error()
			return m, nil
		}

		if m.bodies == nil {
			m.bodies = make(map[bodyKey]*mail.Body)
		}
		m.bodies[key] = msg.body
		m.clampReaderScroll()

		// Opening a message marks it read. The command is a separate STORE, so
		// fetching a body still never changes a flag.
		if m.markSeenOnLoad[key] {
			delete(m.markSeenOnLoad, key)

			return m, m.markSeenCmd(msg.uid, false)
		}

		return m, nil

	case attachmentSavedMsg:
		if msg.err != nil {
			m.status = msg.err.Error()
			return m, nil
		}
		m.status = "saved " + msg.path

		return m, nil

	case flagsMsg:
		if msg.account != m.accountName() || msg.folder != m.openFolder {
			return m, nil
		}

		if msg.err != nil {
			if !msg.quiet {
				m.status = msg.err.Error()
			}
			return m, nil
		}

		m.applyFlags(msg.uids, msg.flags, msg.add)
		m.visualAnchor = noVisualAnchor
		m.status = ""

		return m, nil

	case deleteMsg:
		if msg.account != m.accountName() || msg.folder != m.openFolder {
			return m, nil
		}

		if msg.err != nil {
			if errors.Is(msg.err, mail.ErrAlreadyInTrash) {
				m.status = "already in Trash"
			} else {
				m.status = msg.err.Error()
			}

			return m, nil
		}

		m.removeDeleted(msg.uids)
		m.status = fmt.Sprintf("moved %s to trash", countLabel(len(msg.uids), "message"))

		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)
	}

	return m, nil
}

// handleKey applies a single keypress.
func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.confirmOpen {
		return m.handleConfirmKey(msg)
	}
	if m.attachOpen {
		return m.handleAttachKey(msg)
	}
	if m.foldersOpen {
		return m.handleFoldersKey(msg)
	}
	if m.compose != nil {
		return m.handleComposeKey(msg)
	}

	switch m.keys.resolve(msg.String()) {
	case actionQuit, actionForceQuit:
		return m, tea.Quit

	case actionNextPanel:
		m.focus = m.nextFocus()

	case actionBack:
		if m.visualActive() {
			m.visualAnchor = noVisualAnchor
			break
		}
		switch {
		case m.focus == focusReader:
			m.focus = focusMessages
		case m.focus == focusMessages && ComputeLayout(m.width, m.height).Mode == ModeSingle:
			m.focus = focusFolders
		}
		m.status = ""

	case actionAccount:
		m.status = "account selector arrives in M6"

	case actionCompose:
		return m.beginCompose()

	case actionReply:
		return m.beginReply(false)

	case actionReplyAll:
		return m.beginReply(true)

	case actionForward:
		return m.beginForward()

	case actionReload:
		m.state = stateConnecting
		m.status = ""
		m.messages = nil
		m.rows = nil
		m.openFolder = ""
		m.msgLoading = false
		m.bodies = make(map[bodyKey]*mail.Body)
		m.markSeenOnLoad = make(map[bodyKey]bool)
		m.loadingBody = bodyKey{}
		m.readerScroll = 0
		m.visualAnchor = noVisualAnchor
		m.attachOpen = false
		m.attachCursor = 0
		m.foldersOpen = false
		m.folderOverlayCursor = 0

		return m, m.reloadCmd()

	case actionOpen:
		if m.focus == focusFolders {
			return m.openSelectedFolder()
		}
		if m.focus == focusMessages {
			return m.openSelectedMessage()
		}

	case actionAttachments:
		return m.openAttachments()

	case actionFoldersOverlay:
		return m.openFolders()

	case actionToggleRead:
		return m.toggleRead()

	case actionToggleFlag:
		return m.toggleFlag()

	case actionDelete:
		return m.beginDelete()

	case actionVisualRange:
		m.toggleVisual()

	case actionNextThreadMessage:
		if m.focus == focusReader {
			return m.moveThreadSelection(1)
		}

	case actionPrevThreadMessage:
		if m.focus == focusReader {
			return m.moveThreadSelection(-1)
		}

	case actionUp:
		switch m.focus {
		case focusFolders:
			m.moveFolderCursor(-1)
		case focusMessages:
			m.moveMessageCursor(-1)
		case focusReader:
			m.moveReaderScroll(-1)
		}

	case actionDown:
		switch m.focus {
		case focusFolders:
			m.moveFolderCursor(1)
		case focusMessages:
			m.moveMessageCursor(1)
		case focusReader:
			m.moveReaderScroll(1)
		}

	case actionTop:
		switch m.focus {
		case focusFolders:
			m.folderCursor = 0
			m.clampFolderScroll()
		case focusMessages:
			m.messageCursor = 0
			m.clampMessageScroll()
		case focusReader:
			m.readerScroll = 0
		}

	case actionBottom:
		m.jumpToEnd()
	}

	return m, nil
}

// jumpToEnd moves the active panel's cursor to its last row.
func (m *Model) jumpToEnd() {
	if m.focus == focusFolders && len(m.folders) > 0 {
		m.folderCursor = len(m.folders) - 1
		m.clampFolderScroll()
	}
	if m.focus == focusMessages && len(m.rows) > 0 {
		m.messageCursor = len(m.rows) - 1
		m.clampMessageScroll()
	}
	if m.focus == focusReader {
		total := len(m.readerLines(m.readerInnerWidth()))
		m.readerScroll = max(0, total-m.readerRows())
	}
}

// openSelectedFolder starts loading the folder under the cursor.
func (m Model) openSelectedFolder() (tea.Model, tea.Cmd) {
	folder, ok := m.selectedFolder()
	if !ok {
		return m, nil
	}
	if !folder.Selectable() {
		m.status = fmt.Sprintf("%s is a container, not a mailbox", folder.Name)
		return m, nil
	}

	m.openFolder = folder.Name
	m.messages = nil
	m.rows = nil
	m.messageCursor = 0
	m.messageScroll = 0
	m.msgLoading = true
	m.status = ""
	m.focus = focusMessages
	m.loadingBody = bodyKey{}
	m.readerScroll = 0
	m.visualAnchor = noVisualAnchor
	m.attachOpen = false
	m.attachCursor = 0
	m.foldersOpen = false
	m.folderOverlayCursor = 0

	return m, m.loadMessagesCmd(folder.Name)
}

// openSelectedMessage loads the body for the message under the cursor. The
// fetch is explicit so moving the cursor does not reconnect for every row, and
// focus stays on the list so j/k keeps navigating.
func (m Model) openSelectedMessage() (tea.Model, tea.Cmd) {
	msg, ok := m.selectedMessage()
	if !ok || m.openFolder == "" {
		return m, nil
	}

	key := bodyKey{account: m.accountName(), folder: m.openFolder, uid: msg.UID}

	if len(m.cfg.Accounts) > 0 {
		if drafts, ok := m.draftsFolder(m.cfg.Accounts[m.account]); ok && drafts == m.openFolder {
			m.status = ""
			return m, m.openDraftCmd(m.openFolder, msg.UID)
		}
	}

	m.focus = focusReader
	m.visualAnchor = noVisualAnchor

	if _, cached := m.bodyFor(key); cached {
		return m, m.markSeenCmd(msg.UID, msg.Seen())
	}
	if m.loadingBody == key {
		return m, nil
	}

	m.loadingBody = key
	m.readerScroll = 0
	m.status = ""

	// The body load returns first; the \Seen command follows in bodyMsg. Both
	// cannot be batched here because a batch resolves to one BatchMsg, which
	// makes the follow-up untestable and hides which command failed.
	if m.markSeenOnLoad == nil {
		m.markSeenOnLoad = make(map[bodyKey]bool)
	}
	if msg.Seen() {
		delete(m.markSeenOnLoad, key)
	} else {
		m.markSeenOnLoad[key] = true
	}

	return m, m.loadBodyCmd(key)
}

// moveThreadSelection opens the next or previous message of the conversation the
// cursor is in. It reuses openSelectedMessage, so the body fetch and the
// mark-as-read intent behave exactly as they do for enter.
func (m Model) moveThreadSelection(delta int) (tea.Model, tea.Cmd) {
	thread := m.currentThread()

	for next := m.messageCursor + delta; next >= 0 && next < len(m.rows); next += delta {
		if m.rows[next].Thread != thread {
			continue
		}

		m.messageCursor = next
		m.readerScroll = 0
		m.clampMessageScroll()

		return m.openSelectedMessage()
	}

	return m, nil
}

// loadBodyCmd loads one message body in the background.
func (m Model) loadBodyCmd(key bodyKey) tea.Cmd {
	if len(m.cfg.Accounts) == 0 {
		return nil
	}

	acct := m.cfg.Accounts[m.account]
	load := m.loadBody
	if load == nil {
		return nil
	}

	return func() tea.Msg {
		body, err := load(acct, key.folder, key.uid)

		return bodyMsg{account: acct.Name, folder: key.folder, uid: key.uid, body: body, err: err}
	}
}

// clampFocus moves focus off a panel the current mode does not draw.
func (m *Model) clampFocus() {
	switch ComputeLayout(m.width, m.height).Mode {
	case ModeThree, ModeSingle:
		return
	}

	// ModeTwo shows folders in an overlay, never as a focused panel.
	if m.focus == focusFolders {
		m.focus = focusMessages
	}
}

// nextFocus advances focus to the next panel visible in the current mode.
func (m Model) nextFocus() panelFocus {
	switch ComputeLayout(m.width, m.height).Mode {
	case ModeThree:
		switch m.focus {
		case focusFolders:
			return focusMessages
		case focusMessages:
			return focusReader
		default:
			return focusFolders
		}

	case ModeTwo:
		if m.focus == focusReader {
			return focusMessages
		}

		return focusReader
	}

	// ModeSingle: folders, messages and reader share the single panel.
	switch m.focus {
	case focusFolders:
		return focusMessages
	case focusMessages:
		return focusReader
	default:
		return focusFolders
	}
}

// panelTitle names the pane shown in single-column mode.
func panelTitle(focus panelFocus) string {
	switch focus {
	case focusFolders:
		return "folders"
	case focusMessages:
		return "messages"
	default:
		return "reader"
	}
}

// View renders the whole interface.
func (m Model) View() string {
	if m.width == 0 || m.height == 0 {
		return "chute " + m.version
	}

	layout := ComputeLayout(m.width, m.height)
	panels := m.renderPanels(layout)
	switch {
	case m.confirmOpen:
		if modal := m.renderConfirmModal(ContentWidth(m.width), ContentHeight(m.height)); modal != "" {
			panels = modal
		}
	case m.attachOpen:
		if modal := m.renderAttachmentModal(ContentWidth(m.width), ContentHeight(m.height)); modal != "" {
			panels = modal
		}
	case m.foldersOpen:
		if overlay := m.renderFoldersOverlay(ContentWidth(m.width), ContentHeight(m.height)); overlay != "" {
			panels = overlay
		}
	}

	footer := m.renderFooter()
	spacer := Blank(SpacerHeight)
	status := m.renderStatusBar()

	return AppStyle.Render(JoinVertical(panels, spacer, footer, spacer, status))
}

// renderPanels draws the visible panels for the current mode.
func (m Model) renderPanels(layout Layout) string {
	if m.compose != nil {
		return m.renderComposer(layout)
	}

	readerWidth := max(1, layout.Reader.Width-(2*PanelPaddingHorizontal))
	folderWidth := max(1, layout.Folders.Width-(2*PanelPaddingHorizontal))

	folderBody := m.folderLines(layout.Folders.Height-1, folderWidth)
	messageBody := m.messageLines(layout.Messages.Height-1, layout.Messages.Width-(2*PanelPaddingHorizontal))
	readerBody := m.readerWindow(layout.Reader, m.readerLines(readerWidth))

	switch layout.Mode {
	case ModeThree:
		return JoinHorizontal(
			m.renderPanel("folders", layout.Folders, folderBody, m.focus == focusFolders),
			m.renderPanel("messages", layout.Messages, messageBody, m.focus == focusMessages),
			m.renderPanel("reader", layout.Reader, readerBody, m.focus == focusReader),
		)

	case ModeTwo:
		return JoinHorizontal(
			m.renderPanel("messages", layout.Messages, messageBody, m.focus == focusMessages),
			m.renderPanel("reader", layout.Reader, readerBody, m.focus == focusReader),
		)
	}

	body := m.folderLines(layout.Reader.Height-1, readerWidth)
	if m.focus == focusMessages {
		body = m.messageLines(layout.Reader.Height-1, readerWidth)
	}
	if m.focus == focusReader {
		body = readerBody
	}

	return m.renderPanel(panelTitle(m.focus), layout.Reader, body, true)
}

// renderFooter shows the keys that work right now.
func (m Model) renderFooter() string {
	hints := "q:quit  j/k:move  enter:open  esc:back  \\:folders  m:read  f:flag  d:delete  v:range  s:attach  r:reload"

	switch {
	case m.confirmOpen && m.confirmKind == confirmDiscardDraft:
		hints = "enter:discard  esc:keep editing"
	case m.confirmOpen:
		hints = "enter:delete  esc:cancel"
	case m.compose != nil:
		hints = "ctrl+s:save  ctrl+d:send  tab:field  esc:cancel"
	case m.attachOpen:
		hints = "j/k:choose  enter:save  esc:close"
	case m.foldersOpen:
		hints = "j/k:choose  enter:open  esc:close"
	case m.visualActive():
		hints = "j/k:extend  m:read  f:flag  d:delete  esc:cancel"
	case m.focus == focusReader:
		hints = "n:next  p:prev  j/k:scroll  esc:back  m:read  f:flag  d:delete  s:attach"
	}

	return MutedStyle.Render(Truncate(hints, ContentWidth(m.width)))
}

// renderStatusBar shows the account, connection state, folder count and messages.
func (m Model) renderStatusBar() string {
	parts := []string{"[" + m.accountName() + "]"}

	switch m.state {
	case stateConnecting:
		parts = append(parts, "connecting...")
	case stateConnected:
		parts = append(parts, countLabel(len(m.folders), "folder"))
	case stateFailed:
		parts = append(parts, "disconnected")
	}

	if m.openFolder != "" {
		parts = append(parts, m.openFolder)
		if !m.msgLoading {
			parts = append(parts, countLabel(len(m.messages), "message"))
		}
	}

	if m.status != "" {
		parts = append(parts, m.status)
	}

	line := Truncate(strings.Join(parts, " | "), ContentWidth(m.width))
	if m.state == stateFailed {
		return ErrorStyle.Render(line)
	}

	return MutedStyle.Render(line)
}

// accountName is the active account's name, or an empty string.
func (m Model) accountName() string {
	if m.account < 0 || m.account >= len(m.cfg.Accounts) {
		return ""
	}

	return m.cfg.Accounts[m.account].Name
}

// countLabel renders "1 folder" or "12 folders".
func countLabel(n int, noun string) string {
	if n == 1 {
		return fmt.Sprintf("1 %s", noun)
	}

	return fmt.Sprintf("%d %ss", n, noun)
}
