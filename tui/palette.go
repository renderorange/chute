// Package tui renders the chute interface.
package tui

import "github.com/charmbracelet/lipgloss"

// Palette. Tuned for a light terminal background, matching how imap-notes actually
// paints: body text is dark grey (238). The pale 252 in imap-notes' palette file is
// defined but barely used, and using it as the normal text colour makes everything
// vanish on a white background.
var (
	ColorPrimary     = lipgloss.Color("240") // #585858 borders
	ColorAccent      = lipgloss.Color("238") // #444444 panel titles, bold
	ColorTextNormal  = lipgloss.Color("238") // #444444 body text
	ColorTextMuted   = lipgloss.Color("245") // #888888 secondary text
	ColorTextSuccess = lipgloss.Color("71")  // #66AA66
	ColorTextError   = lipgloss.Color("167") // #CC6666
)

// Chrome heights. The app frame adds one blank line above and below, three columns
// left and right; the footer, spacer and status bar each take one line.
const (
	AppPaddingVertical   = 2
	AppPaddingHorizontal = 6

	FooterHeight    = 1
	SpacerHeight    = 1
	StatusBarHeight = 1
	ChromeHeight    = FooterHeight + SpacerHeight + StatusBarHeight
)

// Panel padding. Unlike imap-notes, vertical padding is applied once, in the
// geometry pass, and never subtracted again inside a panel renderer.
const (
	PanelPaddingVertical   = 1
	PanelPaddingHorizontal = 1
)

// Breakpoints, measured in terminal columns.
const (
	// NarrowBreakpoint is the width below which only one panel fits.
	NarrowBreakpoint = 60

	// ThreeColumnBreakpoint is the width needed for folders, messages and reader.
	ThreeColumnBreakpoint = 100
)

// Column clamps.
const (
	MinFoldersWidth  = 24
	MaxFoldersWidth  = 28
	MinMessagesWidth = 28
	MaxMessagesWidth = 42
	MinReaderWidth   = 30
)

var (
	// AppStyle is the outer frame.
	AppStyle = lipgloss.NewStyle().
			Padding(PanelPaddingVertical, 3)

	// TitleStyle labels a panel.
	TitleStyle = lipgloss.NewStyle().
			Foreground(ColorAccent).
			Bold(true)

	// SelectedStyle marks the focused row or panel.
	SelectedStyle = lipgloss.NewStyle().
			Foreground(ColorTextNormal).
			Bold(true)

	// MutedStyle is for secondary text.
	MutedStyle = lipgloss.NewStyle().
			Foreground(ColorTextMuted)

	// NormalStyle is the default text style.
	NormalStyle = lipgloss.NewStyle().
			Foreground(ColorTextNormal)

	// UnreadStyle marks unread messages: the normal colour, in bold.
	UnreadStyle = lipgloss.NewStyle().
			Foreground(ColorTextNormal).
			Bold(true)

	// ErrorStyle is for failures.
	ErrorStyle = lipgloss.NewStyle().
			Foreground(ColorTextError)

	// SuccessStyle is for confirmations.
	SuccessStyle = lipgloss.NewStyle().
			Foreground(ColorTextSuccess)

	// ModalStyle is the rounded dialog used by confirmations and selectors.
	ModalStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(ColorPrimary).
			Padding(PanelPaddingVertical, 2)
)
