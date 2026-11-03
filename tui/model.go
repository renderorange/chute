package tui

import (
	"errors"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
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

	// collapsed is keyed by a thread root UID: the folder's collapsed conversations.
	// It resets with the folder, because a conversation belongs to one listing.
	collapsed map[imap.UID]bool

	// searchOpen is the server-search prompt; searchActive means the list shows its
	// transient results. searchReturn* remember where the folder list was.
	searchOpen         bool
	searchQuery        string
	searchActive       bool
	searchResults      []mail.Message
	searchReturnCursor int
	searchReturnScroll int

	attachOpen   bool
	attachCursor int

	foldersOpen         bool
	folderOverlayCursor int

	moveOpen    bool
	moveCursor  int
	moveFolders []mail.Folder

	accountsOpen  bool
	accountCursor int
	unseen        map[string]uint32
	unseenErrs    map[string]error

	lastSeen   map[folderKey]imap.UID
	openUnseen uint32
	polling    bool
	clock      tickFunc

	confirmOpen   bool
	confirmKind   confirmKind
	pendingDelete []imap.UID
	pendingEmpty  int

	// filter narrows the loaded list; filterOpen is the prompt that edits it.
	filter     string
	filterOpen bool

	// helpOpen is the keymap overlay; helpScroll is its first visible body line.
	helpOpen   bool
	helpScroll int

	// settingsOpen is the read-only settings report; settingsScroll is its first visible
	// line. configPath and debugPath are what main was started with (#67).
	settingsOpen   bool
	settingsScroll int
	configPath     string
	debugPath      string

	// selection is the last account and folder, remembered across restarts, and
	// saveState persists it. A nil writer means this model keeps no state.
	selection config.State
	saveState func(config.State) error

	compose           *composer
	composeDraftUID   imap.UID
	composeDraftKnown bool

	loadFolders      loadFoldersFunc
	loadMessages     loadMessagesFunc
	loadBody         loadBodyFunc
	storeFlags       storeFlagsFunc
	deleteMessages   deleteMessagesFunc
	moveToRole       moveToRoleFunc
	moveToFolder     moveToFolderFunc
	purgeMessages    purgeMessagesFunc
	emptyTrash       emptyTrashFunc
	fetchCompose     fetchComposeFunc
	saveDraft        saveDraftFunc
	appendSent       appendSentFunc
	sendMessage      sendMessageFunc
	findDraftsFolder draftsFolderFunc
	countUnseen      unseenFunc
	pollFolder       pollFunc
	countUIDs        uidsFunc
	searchMessages   searchFuncs

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
		moveToRole:       load.moveToRole,
		moveToFolder:     load.moveToFolder,
		purgeMessages:    load.purgeMessages,
		emptyTrash:       load.emptyTrash,
		fetchCompose:     load.fetchCompose,
		saveDraft:        load.saveDraft,
		appendSent:       load.appendSent,
		sendMessage:      load.sendMessage,
		findDraftsFolder: load.findDraftsFolder,
		countUnseen:      load.countUnseen,
		pollFolder:       load.pollFolder,
		countUIDs:        load.countUIDs,
		searchMessages:   load.search,
		clock:            tea.Tick,
		bodies:           make(map[bodyKey]*mail.Body),
		markSeenOnLoad:   make(map[bodyKey]bool),
		collapsed:        make(map[imap.UID]bool),
		lastSeen:         make(map[folderKey]imap.UID),
		visualAnchor:     noVisualAnchor,
		keys:             bindingsFromConfig(cfg.KeyBindings()),
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

// Init starts the first folder load and arms the new-mail poll.
func (m Model) Init() tea.Cmd {
	return tea.Batch(m.reloadCmd(), m.schedulePoll())
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

	case unseenMsg:
		m.applyUnseen(msg)

		return m, nil

	case pollTickMsg:
		cmd := m.pollCmd()
		m.polling = cmd != nil

		return m, tea.Batch(cmd, m.schedulePoll())

	case pollMsg:
		return m, m.applyPoll(msg)

	case uidsMsg:
		m.applyUIDs(msg)

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
		m.clampHelpScroll()

		return m, nil

	case foldersMsg:
		if msg.account != m.accountName() {
			return m, nil
		}

		if msg.err != nil {
			m.state = stateFailed
			m.status = statusText(msg.err)
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

			// The folder this account had open last time wins over the configured start
			// folder while the listing still has it. Neither is a change the user just
			// made, so neither writes the state file: someone who never switches
			// anything does not get a new file in their home directory.
			if folder, ok := m.rememberedFolder(); ok {
				m.moveFolderCursorTo(folder)

				return m.openFolderByName(folder.Name, false)
			}
			if folder, ok := m.startFolder(); ok {
				m.moveFolderCursorTo(folder)

				return m.openFolderByName(folder.Name, false)
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
			m.status = statusText(msg.err)
			m.messages = nil
			m.rows = nil

			return m, nil
		}

		m.messages = sortNewestFirst(msg.messages)
		m.rows = m.visibleRows()
		m.messageCursor = 0
		m.messageScroll = 0
		m.visualAnchor = noVisualAnchor
		m.clampMessageScroll()
		m.openUnseen = countUnread(m.messages)
		m.advanceLastSeen(folderKey{account: msg.account, folder: msg.folder})

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
			m.status = statusText(msg.err)
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
			m.status = statusText(msg.err)
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
				m.status = statusText(msg.err)
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
			switch {
			case errors.Is(msg.err, mail.ErrAlreadyInTrash):
				m.status = "already in Trash"
			case errors.Is(msg.err, mail.ErrNoRoleFolder):
				m.status = "no trash folder configured"
			default:
				m.status = statusText(msg.err)
			}

			return m, nil
		}

		m.removeMessages(msg.uids)

		// Set the status after following the delete: opening the neighbouring
		// message clears the status bar, and the move confirmation has to win.
		next, cmd := m.followRows()
		next.status = fmt.Sprintf("moved %s to trash", countLabel(len(msg.uids), "message"))

		return next, cmd

	case moveMsg:
		if msg.account != m.accountName() || msg.folder != m.openFolder {
			return m, nil
		}

		if msg.err != nil {
			m.status = moveStatus(msg)

			return m, nil
		}

		return m.afterMoveSuccess(msg.uids, msg.dest)

	case folderMoveMsg:
		if msg.account != m.accountName() || msg.folder != m.openFolder {
			return m, nil
		}

		if msg.err != nil {
			m.status = folderMoveStatus(msg)

			return m, nil
		}

		return m.afterMoveSuccess(msg.uids, msg.dest)

	case purgeMsg:
		if msg.account != m.accountName() || msg.folder != m.openFolder {
			return m, nil
		}

		if msg.err != nil {
			m.status = statusText(msg.err)

			return m, nil
		}

		return m.afterRemoval(msg.uids, fmt.Sprintf("deleted %s permanently", countLabel(len(msg.uids), "message")))

	case emptyTrashMsg:
		if msg.account != m.accountName() || msg.folder != m.openFolder {
			return m, nil
		}

		if msg.err != nil {
			m.status = statusText(msg.err)

			return m, nil
		}

		return m.afterRemoval(msg.uids, fmt.Sprintf("emptied Trash (%s)", countLabel(len(msg.uids), "message")))

	case searchMsg:
		m.applySearch(msg)

		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)
	}

	return m, nil
}

// handleKey applies a single keypress.
func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Help is checked first: it can be opened from the main view, and closing it must
	// not also run the action the same key has outside the overlay (esc is "back").
	if m.helpOpen {
		return m.handleHelpKey(msg)
	}
	if m.settingsOpen {
		return m.handleSettingsKey(msg)
	}
	if m.accountsOpen {
		return m.handleAccountsKey(msg)
	}
	if m.confirmOpen {
		return m.handleConfirmKey(msg)
	}
	if m.attachOpen {
		return m.handleAttachKey(msg)
	}
	if m.moveOpen {
		return m.handleMoveKey(msg)
	}
	if m.foldersOpen {
		return m.handleFoldersKey(msg)
	}
	if m.compose != nil {
		return m.handleComposeKey(msg)
	}
	if m.searchOpen {
		return m.handleSearchKey(msg)
	}
	if m.filterOpen {
		return m.handleFilterKey(msg)
	}

	switch m.keys.resolve(msg.String()) {
	case actionQuit, actionForceQuit:
		return m, tea.Quit

	case actionNextPanel:
		m.focus = m.nextFocus()

	case actionPrevPanel:
		m.focus = m.prevFocus()

	case actionBack:
		if m.searchActive {
			m.leaveSearch()
			return m, nil
		}
		if m.visualActive() {
			m.visualAnchor = noVisualAnchor
			break
		}
		// esc means "back" everywhere else; on a filtered message list the way back
		// is to the whole folder, so the same key clears the filter.
		if m.filter != "" && m.focus == focusMessages {
			m.filter = ""
			m.applyFilter()
			m.status = ""
			break
		}
		switch {
		case m.focus == focusReader:
			m.focus = focusMessages
		case m.focus == focusMessages && m.layout().Mode == ModeSingle:
			m.focus = focusFolders
		}
		m.status = ""

	case actionAccount:
		return m.openAccounts()

	case actionAccountNext:
		return m.cycleAccount(1)

	case actionAccountPrev:
		return m.cycleAccount(-1)

	case actionEmptyTrash:
		return m.beginEmptyTrash()

	case actionCollapse:
		return m.toggleCollapse()

	case actionSearch:
		return m.beginSearch()

	case actionSettings:
		return m.openSettings()

	case actionHelp:
		m.helpOpen = true
		m.helpScroll = 0

		return m, nil

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
		m.filter = ""
		m.filterOpen = false
		m.msgLoading = false
		m.openUnseen = 0
		m.bodies = make(map[bodyKey]*mail.Body)
		m.markSeenOnLoad = make(map[bodyKey]bool)
		m.loadingBody = bodyKey{}
		m.readerScroll = 0
		m.visualAnchor = noVisualAnchor
		m.collapsed = make(map[imap.UID]bool)
		m.clearSearch()
		m.moveOpen = false
		m.moveFolders = nil
		m.moveCursor = 0

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

	case actionArchive:
		return m.beginArchive()

	case actionJunk:
		return m.beginJunk()

	case actionMove:
		return m.beginMove()

	case actionFilter:
		return m.openFilter()

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

	return m.openFolderByName(folder.Name, true)
}

// openFolderByName starts loading a folder and clears the per-folder view state.
// remember writes the selection: the automatic open at startup is not a change the
// user made, and must not create a state file for someone who switches nothing.
func (m Model) openFolderByName(name string, remember bool) (tea.Model, tea.Cmd) {
	m.openFolder = name
	m.filter = ""
	m.messages = nil
	m.rows = nil
	m.messageCursor = 0
	m.messageScroll = 0
	m.msgLoading = true
	m.openUnseen = 0
	m.status = ""
	m.focus = focusMessages
	m.loadingBody = bodyKey{}
	m.readerScroll = 0
	m.visualAnchor = noVisualAnchor
	m.collapsed = make(map[imap.UID]bool)
	m.clearSearch()
	m.attachOpen = false
	m.attachCursor = 0
	m.foldersOpen = false
	m.folderOverlayCursor = 0

	if remember {
		m.rememberSelection()
	}

	return m, m.loadMessagesCmd(name)
}

// openSelectedMessage loads the body for the message under the cursor. The
// fetch is explicit so moving the cursor does not reconnect for every row, and
// focus stays on the list so the arrows keep navigating.
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
	switch m.layout().Mode {
	case ModeThree, ModeSingle:
		return
	}

	// ModeTwo shows folders in an overlay, never as a focused panel.
	if m.focus == focusFolders {
		m.focus = focusMessages
	}
}

// focusRing lists the panels tab visits in the current mode, in forward order.
// forward and backward cycling both walk this one slice, so the two directions
// cannot disagree about the order: shift+tab is the same ring read the other way.
func (m Model) focusRing() []panelFocus {
	if m.layout().Mode == ModeTwo {
		// ModeTwo shows folders in an overlay, never as a focused panel.
		return []panelFocus{focusMessages, focusReader}
	}

	// ModeThree draws all three panels; ModeSingle shows the same three in the
	// same order inside one panel.
	return []panelFocus{focusFolders, focusMessages, focusReader}
}

// nextFocus advances focus to the next panel visible in the current mode.
func (m Model) nextFocus() panelFocus {
	return m.stepFocus(1)
}

// prevFocus moves focus to the previous panel visible in the current mode.
func (m Model) prevFocus() panelFocus {
	return m.stepFocus(-1)
}

// stepFocus walks the mode's ring by delta and wraps at both ends.
func (m Model) stepFocus(delta int) panelFocus {
	ring := m.focusRing()

	for i, panel := range ring {
		if panel == m.focus {
			return ring[(i+delta+len(ring))%len(ring)]
		}
	}

	// A focus that is not on the ring (stale after a resize) starts the walk from
	// the ring's first panel in either direction.
	return ring[0]
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

// layout sizes the panels for the current terminal and the configured widths. Every pane
// is sized from this one function, so a configured width cannot reach one panel and miss
// another.
func (m Model) layout() Layout {
	return ComputeLayoutWith(m.width, m.height, Widths{
		FoldersPercent:  m.cfg.Layout.FoldersPercent,
		MessagesPercent: m.cfg.Layout.MessagesPercent,
		FoldersWidth:    m.cfg.Layout.FoldersWidth,
		MessagesWidth:   m.cfg.Layout.MessagesWidth,
	})
}

// View renders the whole interface.
func (m Model) View() string {
	if m.width == 0 || m.height == 0 {
		return "chute " + m.version
	}

	layout := m.layout()
	panels := m.renderPanels(layout)
	switch {
	case m.helpOpen:
		if modal := m.renderHelpOverlay(ContentWidth(m.width), ContentHeight(m.height)); modal != "" {
			panels = modal
		}
	case m.settingsOpen:
		if modal := m.renderSettingsModal(ContentWidth(m.width), ContentHeight(m.height)); modal != "" {
			panels = modal
		}
	case m.confirmOpen:
		if modal := m.renderConfirmModal(ContentWidth(m.width), ContentHeight(m.height)); modal != "" {
			panels = modal
		}
	case m.attachOpen:
		if modal := m.renderAttachmentModal(ContentWidth(m.width), ContentHeight(m.height)); modal != "" {
			panels = modal
		}
	case m.accountsOpen:
		if modal := m.renderAccountsModal(ContentWidth(m.width), ContentHeight(m.height)); modal != "" {
			panels = modal
		}
	case m.moveOpen:
		if modal := m.renderMoveModal(ContentWidth(m.width), ContentHeight(m.height)); modal != "" {
			panels = modal
		}
	case m.foldersOpen:
		if overlay := m.renderFoldersOverlay(ContentWidth(m.width), ContentHeight(m.height)); overlay != "" {
			panels = overlay
		}
	case m.searchOpen:
		if modal := m.renderSearchModal(ContentWidth(m.width), ContentHeight(m.height)); modal != "" {
			panels = modal
		}
	case m.filterOpen:
		if modal := m.renderFilterModal(ContentWidth(m.width), ContentHeight(m.height)); modal != "" {
			panels = modal
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

// renderFooter shows the keys that work right now, in priority order, as many as
// fit. The hints come from the binding table (tui/footer.go), so a rebound key
// cannot leave a stale hint behind, and the footer drops whole hints rather than
// cutting one in half.
func (m Model) renderFooter() string {
	return MutedStyle.Render(packHints(m.keys, m.footerHints(), ContentWidth(m.width)))
}

// statusForBar is the status as the bar shows it. Unknown errors keep their wrapped text,
// but the account tag is already at the start of the bar, so the same account's wrapper
// is dropped rather than spending the width on a repeated name. A mapped phrase never
// contains it, and another account's name is left alone.
func (m Model) statusForBar() string {
	name := m.accountName()
	if name == "" {
		return m.status
	}

	return strings.ReplaceAll(m.status, fmt.Sprintf("mail: account %q: ", name), "")
}

// statusSegment is one field of the status bar. drop is the order in which it yields
// space to a status: 1 is dropped first, 0 never. Context the user can live without goes
// before the status, because the status is the reason they are looking at the bar.
type statusSegment struct {
	text string
	drop int
}

// statusLine joins the non-empty segments in bar order.
func statusLine(segments []statusSegment) string {
	parts := make([]string, 0, len(segments))
	for _, segment := range segments {
		if segment.text != "" {
			parts = append(parts, segment.text)
		}
	}

	return strings.Join(parts, " | ")
}

// renderStatusBar shows the account, connection state, folder count and messages. A
// status, when present, outranks the context: unread, then the message count, then the
// folder, then the folder count and the state word are dropped until it fits, and only a
// status wider than the whole bar is truncated (GH-23 ruling).
func (m Model) renderStatusBar() string {
	width := ContentWidth(m.width)

	segments := []statusSegment{{text: "[" + m.accountName() + "]"}}

	switch m.state {
	case stateConnecting:
		segments = append(segments, statusSegment{text: "connecting...", drop: 5})
	case stateConnected:
		segments = append(segments, statusSegment{text: countLabel(len(m.folders), "folder"), drop: 4})
	case stateFailed:
		segments = append(segments, statusSegment{text: "disconnected", drop: 5})
	}

	if m.openFolder != "" {
		segments = append(segments, statusSegment{text: m.openFolder, drop: 3})
		if !m.msgLoading {
			segments = append(segments, statusSegment{text: countLabel(len(m.messages), "message"), drop: 2})
			if m.openUnseen > 0 {
				// "unread" is an adjective, so it does not pluralise.
				segments = append(segments, statusSegment{text: fmt.Sprintf("%d unread", m.openUnseen), drop: 1})
			}
		}
	}

	if m.status != "" {
		segments = append(segments, statusSegment{text: m.statusForBar()})
	}

	line := statusLine(segments)

	for m.status != "" && lipgloss.Width(line) > width {
		dropped := false

		for rank := 1; rank <= 5 && !dropped; rank++ {
			for i := range segments {
				if segments[i].drop == rank && segments[i].text != "" {
					segments[i].text = ""
					dropped = true

					break
				}
			}
		}

		if !dropped {
			break
		}

		line = statusLine(segments)
	}

	// Display cells, not runes: a wide-rune folder or status must not overflow the frame.
	line = TruncateDisplay(line, width)

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

// countUnread counts the messages in a list that do not carry \Seen, so the status
// bar has a number before the first poll returns one.
func countUnread(messages []mail.Message) uint32 {
	var unread uint32
	for i := range messages {
		if !messages[i].Seen() {
			unread++
		}
	}

	return unread
}
