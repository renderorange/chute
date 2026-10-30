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

	loadFolders loadFoldersFunc
}

// New builds the root model for a loaded config.
func New(cfg *config.Config, version string) Model {
	m := Model{
		version:     version,
		cfg:         cfg,
		state:       stateConnecting,
		loadFolders: defaultLoadFolders,
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

// Update handles terminal events and loader results.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.clampFocus()
		m.clampFolderScroll()

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

		return m, m.reloadCmd()

	case "up", "k":
		if m.focus == focusFolders {
			m.moveFolderCursor(-1)
		}

	case "down", "j":
		if m.focus == focusFolders {
			m.moveFolderCursor(1)
		}

	case "home", "g":
		if m.focus == focusFolders {
			m.folderCursor = 0
			m.clampFolderScroll()
		}

	case "end", "G":
		if m.focus == focusFolders && len(m.folders) > 0 {
			m.folderCursor = len(m.folders) - 1
			m.clampFolderScroll()
		}
	}

	return m, nil
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

	switch layout.Mode {
	case ModeThree:
		return JoinHorizontal(
			m.renderPanel("folders", layout.Folders, folderBody),
			m.renderPanel("messages", layout.Messages, m.messageLines()),
			m.renderPanel("reader", layout.Reader, m.readerLines()),
		)

	case ModeTwo:
		return JoinHorizontal(
			m.renderPanel("messages", layout.Messages, m.messageLines()),
			m.renderPanel("reader", layout.Reader, m.readerLines()),
		)
	}

	body := m.folderLines(layout.Reader.Height - 1)
	if m.focus == focusMessages {
		body = m.messageLines()
	}
	if m.focus == focusReader {
		body = m.readerLines()
	}

	return m.renderPanel("chute", layout.Reader, body)
}

func (m Model) messageLines() []string {
	if m.state == stateConnecting {
		return []string{MutedStyle.Render("waiting for folders")}
	}

	return []string{
		MutedStyle.Render(fmt.Sprintf("%d folder(s)", len(m.folders))),
		MutedStyle.Render("message list lands in M2"),
	}
}

func (m Model) readerLines() []string {
	folder, ok := m.selectedFolder()
	if !ok {
		return []string{MutedStyle.Render("no folder selected")}
	}

	role := "folder"
	if folder.Role() != mail.RoleNone {
		role = string(folder.Role())
	}

	return []string{
		NormalStyle.Render(folder.Name),
		MutedStyle.Render(role),
	}
}

// renderFooter shows the keys that work right now.
func (m Model) renderFooter() string {
	hints := "q:quit  tab:next panel  j/k:move  r:reload  a:accounts"

	if ComputeLayout(m.width, m.height).Mode == ModeTwo {
		hints = "q:quit  tab:next panel  r:reload  a:accounts  \\:folders"
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
		parts = append(parts, fmt.Sprintf("%d folders", len(m.folders)))
	case stateFailed:
		parts = append(parts, "disconnected")
	}

	if m.status != "" {
		parts = append(parts, m.status)
	}

	line := Truncate(strings.Join(parts, " "), ContentWidth(m.width))
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
