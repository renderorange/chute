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

	loadFolders  loadFoldersFunc
	loadMessages loadMessagesFunc
}

// New builds the root model for a loaded config.
func New(cfg *config.Config, version string) Model {
	m := Model{
		version:      version,
		cfg:          cfg,
		state:        stateConnecting,
		loadFolders:  defaultLoadFolders,
		loadMessages: defaultLoadMessages,
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

	case tea.KeyMsg:
		return m.handleKey(msg)
	}

	return m, nil
}

// handleKey applies a single keypress.
func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
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

		return m, m.reloadCmd()

	case "enter":
		if m.focus == focusFolders {
			return m.openSelectedFolder()
		}
		if m.focus == focusMessages {
			m.status = "message body arrives in M2b"
		}

	case "up", "k":
		if m.focus == focusFolders {
			m.moveFolderCursor(-1)
		}
		if m.focus == focusMessages {
			m.moveMessageCursor(-1)
		}

	case "down", "j":
		if m.focus == focusFolders {
			m.moveFolderCursor(1)
		}
		if m.focus == focusMessages {
			m.moveMessageCursor(1)
		}

	case "home", "g":
		if m.focus == focusFolders {
			m.folderCursor = 0
			m.clampFolderScroll()
		}
		if m.focus == focusMessages {
			m.messageCursor = 0
			m.clampMessageScroll()
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

	return m, m.loadMessagesCmd(folder.Name)
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
	footer := m.renderFooter()
	spacer := Blank(1)
	status := m.renderStatusBar()

	return AppStyle.Render(JoinVertical(panels, footer, spacer, status))
}

// renderPanels draws the visible panels for the current mode.
func (m Model) renderPanels(layout Layout) string {
	folderBody := m.folderLines(layout.Folders.Height - 1)
	messageBody := m.messageLines(layout.Messages.Height-1, layout.Messages.Width-(2*PanelPaddingHorizontal))

	switch layout.Mode {
	case ModeThree:
		return JoinHorizontal(
			m.renderPanel("folders", layout.Folders, folderBody),
			m.renderPanel("messages", layout.Messages, messageBody),
			m.renderPanel("reader", layout.Reader, m.readerLines()),
		)

	case ModeTwo:
		return JoinHorizontal(
			m.renderPanel("messages", layout.Messages, messageBody),
			m.renderPanel("reader", layout.Reader, m.readerLines()),
		)
	}

	body := m.folderLines(layout.Reader.Height - 1)
	if m.focus == focusMessages {
		body = m.messageLines(layout.Reader.Height-1, layout.Reader.Width-(2*PanelPaddingHorizontal))
	}
	if m.focus == focusReader {
		body = m.readerLines()
	}

	return m.renderPanel("chute", layout.Reader, body)
}

// readerLines shows the selected message's header. The body arrives in M2b.
func (m Model) readerLines() []string {
	msg, ok := m.selectedMessage()
	if !ok {
		return []string{MutedStyle.Render("no message selected")}
	}

	lines := []string{
		NormalStyle.Render(msg.Subject()),
		MutedStyle.Render(fmt.Sprintf("from %s", msg.From())),
		MutedStyle.Render(msg.Date().Local().Format("Mon 02 Jan 2006 15:04")),
		Blank(1),
		MutedStyle.Render("body lands in M2b"),
	}

	return lines
}

// renderFooter shows the keys that work right now.
func (m Model) renderFooter() string {
	hints := "q:quit  tab:panel  j/k:move  enter:open  r:reload"

	if ComputeLayout(m.width, m.height).Mode == ModeTwo {
		hints = "q:quit  tab:panel  j/k:move  enter:open  r:reload  \\:folders"
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
