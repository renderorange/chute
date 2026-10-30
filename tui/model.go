package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
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

	width  int
	height int

	focus  panelFocus
	state  connState
	status string

	folders      []mail.Folder
	folderCursor int
	folderScroll int

	openFolder    string
	messages      []mail.Message
	messageCursor int
	messageScroll int
	msgLoading    bool

	bodies       map[bodyKey]*mail.Body
	loadingBody  bodyKey
	readerScroll int

	attachOpen   bool
	attachCursor int

	loadFolders  loadFoldersFunc
	loadMessages loadMessagesFunc
	loadBody     loadBodyFunc
}

// New builds the root model for a loaded config.
func New(cfg *config.Config, version string) Model {
	m := Model{
		version:      version,
		cfg:          cfg,
		state:        stateConnecting,
		loadFolders:  defaultLoadFolders,
		loadMessages: defaultLoadMessages,
		loadBody:     defaultLoadBody,
		bodies:       make(map[bodyKey]*mail.Body),
	}

	for i := range cfg.Accounts {
		if cfg.Accounts[i].Default {
			m.account = i
			break
		}
	}

	return m
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
		load = defaultLoadFolders
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
		load = defaultLoadMessages
	}

	return func() tea.Msg {
		messages, err := load(acct, folder)
		return messagesMsg{account: acct.Name, folder: folder, messages: messages, err: err}
	}
}

// Update handles terminal events and loader results.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
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
		m.clampFolderScroll()

		return m, nil

	case messagesMsg:
		if msg.account != m.accountName() || msg.folder != m.openFolder {
			return m, nil
		}

		m.msgLoading = false

		if msg.err != nil {
			m.status = msg.err.Error()
			m.messages = nil

			return m, nil
		}

		m.messages = sortNewestFirst(msg.messages)
		m.messageCursor = 0
		m.messageScroll = 0
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
			m.status = msg.err.Error()
			return m, nil
		}

		if m.bodies == nil {
			m.bodies = make(map[bodyKey]*mail.Body)
		}
		m.bodies[key] = msg.body
		m.clampReaderScroll()

		return m, nil

	case attachmentSavedMsg:
		if msg.err != nil {
			m.status = msg.err.Error()
			return m, nil
		}
		m.status = "saved " + msg.path

		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)
	}

	return m, nil
}

// handleKey applies a single keypress.
func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.attachOpen {
		return m.handleAttachKey(msg)
	}

	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit

	case "tab":
		m.focus = m.nextFocus()

	case "esc":
		m.status = ""

	case "a":
		m.status = "account selector arrives in M6"

	case "r":
		m.state = stateConnecting
		m.status = ""
		m.messages = nil
		m.openFolder = ""
		m.msgLoading = false
		m.bodies = make(map[bodyKey]*mail.Body)
		m.loadingBody = bodyKey{}
		m.readerScroll = 0
		m.attachOpen = false
		m.attachCursor = 0

		return m, m.reloadCmd()

	case "enter":
		if m.focus == focusFolders {
			return m.openSelectedFolder()
		}
		if m.focus == focusMessages {
			return m.openSelectedMessage()
		}

	case "s":
		return m.openAttachments()

	case "up", "k":
		switch m.focus {
		case focusFolders:
			m.moveFolderCursor(-1)
		case focusMessages:
			m.moveMessageCursor(-1)
		case focusReader:
			m.moveReaderScroll(-1)
		}

	case "down", "j":
		switch m.focus {
		case focusFolders:
			m.moveFolderCursor(1)
		case focusMessages:
			m.moveMessageCursor(1)
		case focusReader:
			m.moveReaderScroll(1)
		}

	case "home", "g":
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

	case "end", "G":
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
	if m.focus == focusMessages && len(m.messages) > 0 {
		m.messageCursor = len(m.messages) - 1
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
	m.messageCursor = 0
	m.messageScroll = 0
	m.msgLoading = true
	m.status = ""
	m.focus = focusMessages
	m.loadingBody = bodyKey{}
	m.readerScroll = 0
	m.attachOpen = false
	m.attachCursor = 0

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
	if _, cached := m.bodyFor(key); cached {
		return m, nil
	}
	if m.loadingBody == key {
		return m, nil
	}

	m.loadingBody = key
	m.readerScroll = 0
	m.status = ""

	return m, m.loadBodyCmd(key)
}

// loadBodyCmd loads one message body in the background.
func (m Model) loadBodyCmd(key bodyKey) tea.Cmd {
	if len(m.cfg.Accounts) == 0 {
		return nil
	}

	acct := m.cfg.Accounts[m.account]
	load := m.loadBody
	if load == nil {
		load = defaultLoadBody
	}

	return func() tea.Msg {
		body, err := load(acct, key.folder, key.uid)

		return bodyMsg{account: acct.Name, folder: key.folder, uid: key.uid, body: body, err: err}
	}
}

// clampFocus moves focus off a panel the current mode does not draw.
func (m *Model) clampFocus() {
	switch ComputeLayout(m.width, m.height).Mode {
	case ModeThree:
		return
	}

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

	// ModeSingle: messages and reader share the single panel.
	if m.focus == focusReader {
		return focusMessages
	}

	return focusReader
}

// View renders the whole interface.
func (m Model) View() string {
	if m.width == 0 || m.height == 0 {
		return "chute " + m.version
	}

	layout := ComputeLayout(m.width, m.height)
	panels := m.renderPanels(layout)
	if m.attachOpen {
		if modal := m.renderAttachmentModal(ContentWidth(m.width), ContentHeight(m.height)); modal != "" {
			panels = modal
		}
	}

	footer := m.renderFooter()
	spacer := Blank(1)
	status := m.renderStatusBar()

	return AppStyle.Render(JoinVertical(panels, footer, spacer, status))
}

// renderPanels draws the visible panels for the current mode.
func (m Model) renderPanels(layout Layout) string {
	folderBody := m.folderLines(layout.Folders.Height - 1)
	messageBody := m.messageLines(layout.Messages.Height-1, layout.Messages.Width-(2*PanelPaddingHorizontal))

	readerWidth := max(1, layout.Reader.Width-(2*PanelPaddingHorizontal))
	readerBody := m.readerWindow(layout.Reader, m.readerLines(readerWidth))

	switch layout.Mode {
	case ModeThree:
		return JoinHorizontal(
			m.renderPanel("folders", layout.Folders, folderBody),
			m.renderPanel("messages", layout.Messages, messageBody),
			m.renderPanel("reader", layout.Reader, readerBody),
		)

	case ModeTwo:
		return JoinHorizontal(
			m.renderPanel("messages", layout.Messages, messageBody),
			m.renderPanel("reader", layout.Reader, readerBody),
		)
	}

	body := m.folderLines(layout.Reader.Height - 1)
	if m.focus == focusMessages {
		body = m.messageLines(layout.Reader.Height-1, layout.Reader.Width-(2*PanelPaddingHorizontal))
	}
	if m.focus == focusReader {
		body = readerBody
	}

	return m.renderPanel("chute", layout.Reader, body)
}

// renderFooter shows the keys that work right now.
func (m Model) renderFooter() string {
	hints := "q:quit  tab:panel  j/k:move  enter:open  s:attachments  r:reload"

	switch {
	case m.attachOpen:
		hints = "j/k:choose  enter:save  esc:close"
	case ComputeLayout(m.width, m.height).Mode == ModeTwo:
		hints = "q:quit  tab:panel  j/k:move  enter:open  s:attachments  r:reload  \\:folders"
	}

	return MutedStyle.Render(Truncate(hints, ContentWidth(m.width)))
}

// renderStatusBar shows the account, connection state, folder count and messages.
func (m Model) renderStatusBar() string {
	parts := []string{"[" + m.accountName() + "]"}

	switch m.state {
	case stateConnecting:
		parts = append(parts, "connecting…")
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

	line := Truncate(strings.Join(parts, " · "), ContentWidth(m.width))
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
