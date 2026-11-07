package tui

// Mode is how many panels fit at the current terminal width.
type Mode int

const (
	// ModeThree shows folders, messages and reader side by side.
	ModeThree Mode = iota

	// ModeTwo shows messages and reader; folders move to a modal.
	ModeTwo

	// ModeSingle shows one panel at a time.
	ModeSingle
)

// Box is a panel rectangle inside the padded app frame.
type Box struct {
	X      int
	Y      int
	Width  int
	Height int
}

// Empty reports whether the box has no area to draw in.
func (b Box) Empty() bool {
	return b.Width <= 0 || b.Height <= 0
}

// Layout holds the panel rectangles for one frame.
//
// A box that is not visible in the current mode is the zero Box. In ModeSingle the
// reader box covers the whole content area and the caller decides what to draw
// inside it.
type Layout struct {
	Mode Mode

	Folders  Box
	Messages Box
	Reader   Box
}

// ContentWidth is the width inside the app frame.
func ContentWidth(width int) int {
	w := width - AppPaddingHorizontal
	if w < 1 {
		return 1
	}

	return w
}

// ContentHeight is the height inside the app frame, above the chrome.
func ContentHeight(height int) int {
	h := height - AppPaddingVertical - ChromeHeight
	if h < 1 {
		return 1
	}

	return h
}

// Widths is the configured panel sizing, in the shape config.Layout provides. A zero
// field means automatic: the built-in rule for that pane decides. A non-zero width wins
// over the matching percentage.
type Widths struct {
	FoldersPercent  int
	MessagesPercent int
	FoldersWidth    int
	MessagesWidth   int
}

// Automatic proportions. Folders keep a window because folder names are short; the message
// list has only a floor, so it grows with the terminal.
const (
	autoFoldersPercent     = 25
	autoMessagesPercentT   = 33
	autoMessagesPercentTwo = 45
)

// ComputeLayout sizes the panels for a terminal of the given dimensions, using the built-in
// width rules.
func ComputeLayout(width, height int) Layout {
	return ComputeLayoutWith(width, height, Widths{})
}

// ComputeLayoutWith sizes the panels for a terminal of the given dimensions and a
// configured sizing.
//
// The pane widths plus the PanelGap columns between them always sum to ContentWidth:
// the reader absorbs whatever the other panels do not take, and it keeps
// MinReaderWidth by shrinking the message list first and the folder pane second. A
// configured width that cannot fit is trimmed to those floors rather than rejected,
// because the same config is used at every terminal size.
func ComputeLayoutWith(width, height int, widths Widths) Layout {
	w := ContentWidth(width)
	h := ContentHeight(height)

	switch {
	case width >= ThreeColumnBreakpoint:
		folders := folderColumns(w, widths)
		messages := messageColumns(w, widths, autoMessagesPercentT)

		gaps := 2 * PanelGap

		// Give the reader its minimum back, from the list first and the folders last.
		if short := MinReaderWidth - (w - folders - messages - gaps); short > 0 {
			cut := min(short, messages-MinMessagesWidth)
			messages -= cut
			short -= cut

			if short > 0 {
				folders -= min(short, folders-MinFoldersWidth)
			}
		}
		if rest := w - folders - messages - gaps; rest < 1 {
			messages = max(0, w-folders-1-gaps)
		}

		return Layout{
			Mode:     ModeThree,
			Folders:  Box{X: 0, Y: 0, Width: folders, Height: h},
			Messages: Box{X: folders + PanelGap, Y: 0, Width: messages, Height: h},
			Reader: Box{
				X:      folders + PanelGap + messages + PanelGap,
				Y:      0,
				Width:  w - folders - messages - gaps,
				Height: h,
			},
		}

	case width >= NarrowBreakpoint:
		messages := messageColumns(w, widths, autoMessagesPercentTwo)
		if w-messages-PanelGap < MinReaderWidth {
			messages = max(0, w-MinReaderWidth-PanelGap)
		}
		if messages > w-PanelGap-1 {
			messages = max(0, w-PanelGap-1)
		}

		return Layout{
			Mode:     ModeTwo,
			Messages: Box{X: 0, Y: 0, Width: messages, Height: h},
			Reader:   Box{X: messages + PanelGap, Y: 0, Width: w - messages - PanelGap, Height: h},
		}
	}

	return Layout{
		Mode:   ModeSingle,
		Reader: Box{X: 0, Y: 0, Width: w, Height: h},
	}
}

// folderColumns sizes the folder pane: configured columns, then a configured percentage,
// then the automatic quarter of the content width, kept inside its window because folder
// names are short and a wide folder pane wastes the screen.
func folderColumns(w int, widths Widths) int {
	switch {
	case widths.FoldersWidth > 0:
		return max(widths.FoldersWidth, MinFoldersWidth)
	case widths.FoldersPercent > 0:
		return max(w*widths.FoldersPercent/100, MinFoldersWidth)
	}

	return clamp(w*autoFoldersPercent/100, MinFoldersWidth, MaxFoldersWidth)
}

// messageColumns sizes the message list: configured columns, then a configured percentage,
// then the automatic percentage for this mode. It has a floor and no ceiling, so the list
// grows with the terminal instead of pinning at a maximum.
func messageColumns(w int, widths Widths, autoPercent int) int {
	switch {
	case widths.MessagesWidth > 0:
		return max(widths.MessagesWidth, MinMessagesWidth)
	case widths.MessagesPercent > 0:
		return max(w*widths.MessagesPercent/100, MinMessagesWidth)
	}

	return max(w*autoPercent/100, MinMessagesWidth)
}

func clamp(v, low, high int) int {
	if v < low {
		return low
	}
	if v > high {
		return high
	}

	return v
}

func min(a, b int) int {
	if a < b {
		return a
	}

	return b
}
