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

// blankBlock returns width blank cells over height lines, for the gap between panels.
// JoinHorizontal pads a short part with empty lines, and an empty line is zero cells
// wide, so the gap has to be a real block or every row after the first loses its width.
func blankBlock(width, height int) string {
	if width < 1 || height < 1 {
		return ""
	}

	line := strings.Repeat(" ", width)
	lines := make([]string, height)
	for i := range lines {
		lines[i] = line
	}

	return strings.Join(lines, "\n")
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

// sanitizeDisplay cleans one line of text before it is styled and drawn. It removes
// characters whose cell width a terminal does not agree on with go-runewidth and
// x/ansi, and folds Unicode spaces to an ASCII space.
//
// U+00AD is the one that bites in practice: a terminal advances one cell for a soft
// hyphen while the width tables count zero, so a line lipgloss pads to the frame width
// is one cell wider for every soft hyphen on it. The terminal wraps it, the view becomes
// taller than the window, and bubbletea keeps the last height lines, which reads as every
// pane jumping up. Marketing mail is full of soft hyphens, zero-width joiners and
// no-break spaces used as invisible spacers.
//
// Call this before adding ANSI styling: it drops control characters, but keeps ESC so a
// string that already carries the composer's cursor highlight survives. Mail text never
// reaches here styled; sanitizeText is the strict variant used for message bodies.
func sanitizeDisplay(s string) string {
	return strings.Map(func(r rune) rune {
		switch r {
		case '\r', '\n', '\t', '\v', '\f':
			return ' '
		case '\x1b':
			return r
		}

		return cleanRune(r)
	}, s)
}

// sanitizeText is sanitizeDisplay for a multi-line body: line breaks and tabs are kept,
// so paragraph structure survives and wrapLine can still expand a tab.
func sanitizeText(s string) string {
	return strings.Map(cleanRune, s)
}

// cleanRune drops one rune that must never reach the terminal, or folds a Unicode space
// to an ASCII one. Line breaks and tabs are the caller's decision.
func cleanRune(r rune) rune {
	if r == '\n' || r == '\t' {
		return r
	}

	// C0, DEL and C1 controls: some terminals treat a vertical tab or NEL as a line
	// break, which is the same frame-height hazard as a soft hyphen.
	if r < 0x20 || r == 0x7f || (r >= 0x80 && r <= 0x9f) {
		return -1
	}

	switch r {
	case '\u00ad', // soft hyphen: one cell in a terminal, zero in the width tables
		'\u034f',                     // combining grapheme joiner
		'\u061c',                     // Arabic letter mark
		'\u180e',                     // Mongolian vowel separator
		'\u200b', '\u200c', '\u200d', // zero-width space, non-joiner, joiner
		'\u200e', '\u200f', // left-to-right and right-to-left marks
		'\u202a', '\u202b', '\u202c', '\u202d', '\u202e', // bidi embedding and override
		'\u2060', '\u2061', '\u2062', '\u2063', '\u2064', // word joiner and invisible operators
		'\u2066', '\u2067', '\u2068', '\u2069', // bidi isolates
		'\ufeff',                     // zero-width no-break space
		'\ufff9', '\ufffa', '\ufffb': // interlinear annotation
		return -1
	case '\u00a0', // no-break space
		'\u1680',                                                                                                     // ogham space mark
		'\u2000', '\u2001', '\u2002', '\u2003', '\u2004', '\u2005', '\u2006', '\u2007', '\u2008', '\u2009', '\u200a', // en/em/thin/hair spaces
		'\u202f', // narrow no-break space
		'\u205f', // medium mathematical space
		'\u3000': // ideographic space
		return ' '
	}

	return r
}

// Truncate shortens a plain string to width runes, appending an ellipsis when cut.
func Truncate(s string, width int) string {
	s = sanitizeDisplay(s)
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
	s = sanitizeDisplay(s)
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
// list row, and drops characters the terminal measures differently.
func singleLine(s string) string {
	return sanitizeDisplay(s)
}
