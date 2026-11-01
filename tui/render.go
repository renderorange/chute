package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"
)

// renderPanel draws one panel inside its box: a title line, then the body, padded
// to the box size. Vertical geometry comes from the box, never recomputed here.
//
// A focused panel says so in its title, with a character rather than colour alone: the
// triage keys are inert outside the message list, and without this the user cannot tell
// why a keypress did nothing.
func (m Model) renderPanel(title string, box Box, body []string, focused bool) string {
	if box.Empty() {
		return ""
	}

	// lipgloss Width includes padding: it word-wraps at width minus the horizontal
	// padding. Size the block to the whole box so the content area is exactly
	// box.Width - 2*PanelPaddingHorizontal, matching the width the body was built
	// for. Sizing it to the content width instead would wrap every full row.
	innerWidth := box.Width - (2 * PanelPaddingHorizontal)
	if innerWidth < 1 {
		innerWidth = 1
	}

	lines := make([]string, 0, box.Height)
	titleStyle, label := MutedStyle, title
	if focused {
		titleStyle, label = TitleStyle, "> "+title
	}

	lines = append(lines, titleStyle.Render(TruncateDisplay(label, innerWidth)))

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
		Width(box.Width).
		MaxWidth(box.Width).
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

// ellipsis is the ASCII ellipsis used when truncating a line.
const ellipsis = "..."

// Truncate shortens a plain string to width runes, appending an ellipsis when cut.
func Truncate(s string, width int) string {
	if width < 1 {
		return ""
	}

	runes := []rune(s)
	if len(runes) <= width {
		return s
	}
	if width <= len(ellipsis) {
		return string(runes[:width])
	}

	return string(runes[:width-len(ellipsis)]) + ellipsis
}

// TruncateDisplay shortens a string to width display cells, counting wide runes
// as two cells, and appends an ellipsis when cut.
func TruncateDisplay(s string, width int) string {
	if width < 1 {
		return ""
	}
	if lipgloss.Width(s) <= width {
		return s
	}

	limit := width - len(ellipsis)
	if limit < 1 {
		return firstCells(s, width)
	}

	return firstCells(s, limit) + ellipsis
}

// firstCells returns the longest prefix of s that fits in width display cells.
func firstCells(s string, width int) string {
	var b strings.Builder
	used := 0
	for _, r := range s {
		w := runewidth.RuneWidth(r)
		if used+w > width {
			break
		}
		b.WriteRune(r)
		used += w
	}

	return b.String()
}

// padColumn truncates s to width display cells and pads it back to that width.
// Unlike fmt's %-*s, it measures wide runes correctly.
func padColumn(s string, width int) string {
	s = TruncateDisplay(s, width)
	if pad := width - lipgloss.Width(s); pad > 0 {
		s += strings.Repeat(" ", pad)
	}

	return s
}

// singleLine replaces embedded line breaks and tabs so a header cannot break a
// list row.
func singleLine(s string) string {
	return strings.Map(func(r rune) rune {
		switch r {
		case '\r', '\n', '\t':
			return ' '
		}

		return r
	}, s)
}
