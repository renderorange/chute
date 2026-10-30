package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// renderPanel draws one panel inside its box: a title line, then the body, padded
// to the box size. Vertical geometry comes from the box, never recomputed here.
func (m Model) renderPanel(title string, box Box, body []string) string {
	if box.Empty() {
		return ""
	}

	innerWidth := box.Width - (2 * PanelPaddingHorizontal)
	if innerWidth < 1 {
		innerWidth = 1
	}

	lines := make([]string, 0, box.Height)
	lines = append(lines, TitleStyle.Render(title))

	for _, line := range body {
		if len(lines) >= box.Height {
			break
		}
		lines = append(lines, line)
	}
	for len(lines) < box.Height {
		lines = append(lines, "")
	}

	return lipgloss.NewStyle().
		Width(innerWidth).
		MaxWidth(innerWidth).
		Height(box.Height).
		MaxHeight(box.Height).
		Padding(0, PanelPaddingHorizontal).
		Render(strings.Join(lines, "\n"))
}

// JoinHorizontal places panels side by side, top aligned.
func JoinHorizontal(parts ...string) string {
	return lipgloss.JoinHorizontal(lipgloss.Top, parts...)
}

// JoinVertical stacks blocks, left aligned.
func JoinVertical(parts ...string) string {
	return lipgloss.JoinVertical(lipgloss.Left, parts...)
}

// Blank returns n empty lines.
func Blank(n int) string {
	if n < 1 {
		return ""
	}

	return strings.Repeat("\n", n-1)
}

// Truncate shortens a plain string to width runes, appending an ellipsis when cut.
func Truncate(s string, width int) string {
	if width < 1 {
		return ""
	}

	runes := []rune(s)
	if len(runes) <= width {
		return s
	}
	if width == 1 {
		return string(runes[:1])
	}

	return string(runes[:width-1]) + "…"
}
