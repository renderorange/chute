package tui

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/renderorange/chute/config"
)

type panelFocus int

const (
	focusFolders panelFocus = iota
	focusMessages
	focusReader
)

// Model is the root bubbletea model.
type Model struct {
	version string
	cfg     *config.Config
	account int

	width  int
	height int

	focus  panelFocus
	status string
}

// New builds the root model for a loaded config.
func New(cfg *config.Config, version string) Model {
	return Model{
		version: version,
		cfg:     cfg,
	}
}

// Init satisfies tea.Model. No command runs until the IMAP client exists.
func (m Model) Init() tea.Cmd {
	return nil
}

// Update handles terminal events.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.clampFocus()

		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "tab":
			m.focus = m.nextFocus()
			return m, nil
		case "a":
			m.status = "account selector arrives in M6"
			return m, nil
		case "esc":
			m.status = ""
			return m, nil
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
	switch layout.Mode {
	case ModeThree:
		return JoinHorizontal(
			m.renderPanel("folders", layout.Folders, m.folderLines()),
			m.renderPanel("messages", layout.Messages, m.messageLines()),
			m.renderPanel("reader", layout.Reader, m.readerLines()),
		)

	case ModeTwo:
		return JoinHorizontal(
			m.renderPanel("messages", layout.Messages, m.messageLines()),
			m.renderPanel("reader", layout.Reader, m.readerLines()),
		)
	}

	body := m.messageLines()
	if m.focus == focusReader {
		body = m.readerLines()
	}

	return m.renderPanel("chute", layout.Reader, body)
}

func (m Model) folderLines() []string {
	return []string{
		MutedStyle.Render("no folders yet"),
		MutedStyle.Render("IMAP connect lands in M1b"),
	}
}

func (m Model) messageLines() []string {
	return []string{
		MutedStyle.Render(fmt.Sprintf("%d account(s) configured", len(m.cfg.Accounts))),
		MutedStyle.Render("message list lands in M2"),
	}
}

func (m Model) readerLines() []string {
	return []string{
		NormalStyle.Render(m.version),
		MutedStyle.Render("reader lands in M2"),
	}
}

// renderFooter shows the keys that work right now.
func (m Model) renderFooter() string {
	hints := "q:quit  tab:next panel  a:accounts"

	mode := ComputeLayout(m.width, m.height).Mode
	if mode == ModeTwo {
		hints = "q:quit  tab:next panel  a:accounts  \\:folders"
	}
	if mode == ModeSingle {
		hints = "q:quit  tab:next panel  a:accounts"
	}

	return MutedStyle.Render(Truncate(hints, ContentWidth(m.width)))
}

// renderStatusBar shows the active account and any transient status message.
func (m Model) renderStatusBar() string {
	name := ""
	if m.account < len(m.cfg.Accounts) {
		name = m.cfg.Accounts[m.account].Name
	}

	left := fmt.Sprintf("[%s] %s", name, m.status)
	if m.status != "" {
		return ErrorStyle.Render(Truncate(left, ContentWidth(m.width)))
	}

	return MutedStyle.Render(Truncate(left, ContentWidth(m.width)))
}
