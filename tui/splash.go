package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// splashConnecting is the status line the splash shows while the first folder
// listing is in flight. The host is printed when the config names one, which a real
// account always does; a hand-built test model may not.
func (m Model) splashConnecting() string {
	account := m.accountName()
	if account == "" {
		return "connecting..."
	}

	line := "[" + account + "] connecting"

	if m.account >= 0 && m.account < len(m.cfg.Accounts) {
		if host := m.cfg.Accounts[m.account].IMAP.Host; host != "" {
			line += " to " + host
		}
	}

	return line + "..."
}

// renderSplash draws the startup screen: what the app is, which build, the account
// and host it is dialling, and how to leave the screen. It fills the content area so
// the frame is the same size as every other screen, and lipgloss.Place pads it to
// exactly that size.
func (m Model) renderSplash() string {
	width := max(1, m.width-AppPaddingHorizontal)
	height := max(1, m.height-AppPaddingVertical)

	version := m.version
	if version == "" {
		version = "dev"
	}

	block := strings.Join([]string{
		TitleStyle.Render("chute"),
		MutedStyle.Render(version),
		"",
		MutedStyle.Render(TruncateDisplay(m.splashConnecting(), width)),
		MutedStyle.Render("press any key to skip"),
	}, "\n")

	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, block)
}
