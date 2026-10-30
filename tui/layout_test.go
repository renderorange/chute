package tui

import "testing"

func TestComputeLayoutThreeColumns(t *testing.T) {
	layout := ComputeLayout(140, 40)

	if layout.Mode != ModeThree {
		t.Fatalf("mode = %v, want ModeThree", layout.Mode)
	}

	available := ContentWidth(140)
	if got := layout.Folders.Width + layout.Messages.Width + layout.Reader.Width; got != available {
		t.Errorf("column widths sum to %d, want %d", got, available)
	}
	if layout.Folders.Width < MinFoldersWidth || layout.Folders.Width > MaxFoldersWidth {
		t.Errorf("folders width = %d, want %d..%d", layout.Folders.Width, MinFoldersWidth, MaxFoldersWidth)
	}
	if layout.Messages.Width < MinMessagesWidth || layout.Messages.Width > MaxMessagesWidth {
		t.Errorf("messages width = %d, want %d..%d", layout.Messages.Width, MinMessagesWidth, MaxMessagesWidth)
	}
	if layout.Reader.Width < MinReaderWidth {
		t.Errorf("reader width = %d, want at least %d", layout.Reader.Width, MinReaderWidth)
	}
	if layout.Folders.X != 0 || layout.Messages.X != layout.Folders.Width {
		t.Errorf("x offsets = %d, %d; want 0, %d", layout.Folders.X, layout.Messages.X, layout.Folders.Width)
	}
	if got, want := layout.Reader.X, layout.Messages.X+layout.Messages.Width; got != want {
		t.Errorf("reader x = %d, want %d", got, want)
	}
}

func TestComputeLayoutThreeColumnsAtBreakpoint(t *testing.T) {
	layout := ComputeLayout(ThreeColumnBreakpoint, 40)

	if layout.Mode != ModeThree {
		t.Fatalf("mode = %v at %d columns, want ModeThree", layout.Mode, ThreeColumnBreakpoint)
	}
	if layout.Reader.Width < MinReaderWidth {
		t.Errorf("reader width = %d at the breakpoint, want at least %d", layout.Reader.Width, MinReaderWidth)
	}
	if got, want := layout.Folders.Width+layout.Messages.Width+layout.Reader.Width, ContentWidth(ThreeColumnBreakpoint); got != want {
		t.Errorf("column widths sum to %d, want %d", got, want)
	}
}

func TestThreeColumnThreshold(t *testing.T) {
	if got := ComputeLayout(ThreeColumnBreakpoint-1, 40).Mode; got != ModeTwo {
		t.Errorf("mode at %d columns = %v, want ModeTwo", ThreeColumnBreakpoint-1, got)
	}
	if got := ComputeLayout(ThreeColumnBreakpoint, 40).Mode; got != ModeThree {
		t.Errorf("mode at %d columns = %v, want ModeThree", ThreeColumnBreakpoint, got)
	}
}

func TestComputeLayoutTwoColumns(t *testing.T) {
	for _, width := range []int{60, 80, 99, 119} {
		layout := ComputeLayout(width, 30)

		if layout.Mode != ModeTwo {
			t.Fatalf("width %d: mode = %v, want ModeTwo", width, layout.Mode)
		}
		if !layout.Folders.Empty() {
			t.Errorf("width %d: folders box should be empty, got %+v", width, layout.Folders)
		}
		if got, want := layout.Messages.Width+layout.Reader.Width, ContentWidth(width); got != want {
			t.Errorf("width %d: columns sum to %d, want %d", width, got, want)
		}
		if layout.Reader.Width < 1 {
			t.Errorf("width %d: reader width = %d, want a positive width", width, layout.Reader.Width)
		}
	}
}

func TestComputeLayoutSingle(t *testing.T) {
	layout := ComputeLayout(59, 24)

	if layout.Mode != ModeSingle {
		t.Fatalf("mode = %v, want ModeSingle", layout.Mode)
	}
	if !layout.Folders.Empty() || !layout.Messages.Empty() {
		t.Error("single mode should leave folders and messages empty")
	}
	if got, want := layout.Reader.Width, ContentWidth(59); got != want {
		t.Errorf("reader width = %d, want %d", got, want)
	}
	if got, want := layout.Reader.Height, ContentHeight(24); got != want {
		t.Errorf("reader height = %d, want %d", got, want)
	}
}

func TestContentHeightMinimum(t *testing.T) {
	layout := ComputeLayout(40, 4)

	if layout.Reader.Height != 1 {
		t.Errorf("reader height = %d, want clamped to 1", layout.Reader.Height)
	}
}

func TestContentWidthMinimum(t *testing.T) {
	layout := ComputeLayout(3, 24)

	if layout.Reader.Width != 1 {
		t.Errorf("reader width = %d, want clamped to 1", layout.Reader.Width)
	}
}

func TestComputeLayoutHeightsMatch(t *testing.T) {
	for _, width := range []int{40, 60, 100, 200} {
		layout := ComputeLayout(width, 50)

		if layout.Reader.Height != ContentHeight(50) {
			t.Errorf("width %d: reader height = %d, want %d", width, layout.Reader.Height, ContentHeight(50))
		}
		if !layout.Messages.Empty() && layout.Messages.Height != ContentHeight(50) {
			t.Errorf("width %d: messages height = %d, want %d", width, layout.Messages.Height, ContentHeight(50))
		}
	}
}
