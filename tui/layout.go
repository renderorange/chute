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

// ComputeLayout sizes the panels for a terminal of the given dimensions.
//
// Widths always sum to ContentWidth: the reader absorbs whatever the other panels
// do not take.
func ComputeLayout(width, height int) Layout {
	w := ContentWidth(width)
	h := ContentHeight(height)

	switch {
	case width >= ThreeColumnBreakpoint:
		folders := clamp(w/4, MinFoldersWidth, MaxFoldersWidth)
		messages := clamp(w/3, MinMessagesWidth, MaxMessagesWidth)

		// Give the reader its minimum back from the messages column first.
		if w-folders-messages < MinReaderWidth {
			messages = clamp(w-folders-MinReaderWidth, MinMessagesWidth, MaxMessagesWidth)
		}
		if rest := w - folders - messages; rest < 1 {
			messages = max(0, w-folders-1)
		}

		return Layout{
			Mode:     ModeThree,
			Folders:  Box{X: 0, Y: 0, Width: folders, Height: h},
			Messages: Box{X: folders, Y: 0, Width: messages, Height: h},
			Reader:   Box{X: folders + messages, Y: 0, Width: w - folders - messages, Height: h},
		}

	case width >= NarrowBreakpoint:
		messages := clamp(w/3, MinMessagesWidth, MaxMessagesWidth)
		if w-messages < MinReaderWidth {
			messages = max(0, w-MinReaderWidth)
		}
		if messages > w-1 {
			messages = max(0, w-1)
		}

		return Layout{
			Mode:     ModeTwo,
			Messages: Box{X: 0, Y: 0, Width: messages, Height: h},
			Reader:   Box{X: messages, Y: 0, Width: w - messages, Height: h},
		}
	}

	return Layout{
		Mode:   ModeSingle,
		Reader: Box{X: 0, Y: 0, Width: w, Height: h},
	}
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

func max(a, b int) int {
	if a > b {
		return a
	}

	return b
}
