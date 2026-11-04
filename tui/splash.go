package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// renderSplash draws the startup screen: the app name and the build version, and
// nothing else. It fills the content area so the frame is the same size as every
// other screen, and lipgloss.Place pads it to exactly that size.
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
	}, "\n")

	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, block)
}
