package tui

import "testing"

func TestComputeLayoutThreeColumns(t *testing.T) {
	layout := ComputeLayout(140, 40)

	if layout.Mode != ModeThree {
		t.Fatalf("mode = %v, want ModeThree", layout.Mode)
	}

	available := ContentWidth(140)
	if got := layout.Folders.Width + layout.Messages.Width + layout.Reader.Width + 2*PanelGap; got != available {
		t.Errorf("column widths plus gaps sum to %d, want %d", got, available)
	}
	if layout.Folders.Width < MinFoldersWidth || layout.Folders.Width > MaxFoldersWidth {
		t.Errorf("folders width = %d, want %d..%d", layout.Folders.Width, MinFoldersWidth, MaxFoldersWidth)
	}
	if layout.Messages.Width < MinMessagesWidth {
		t.Errorf("messages width = %d, want at least %d", layout.Messages.Width, MinMessagesWidth)
	}
	if layout.Reader.Width < MinReaderWidth {
		t.Errorf("reader width = %d, want at least %d", layout.Reader.Width, MinReaderWidth)
	}
	if layout.Folders.X != 0 || layout.Messages.X != layout.Folders.Width+PanelGap {
		t.Errorf("x offsets = %d, %d; want 0, %d",
			layout.Folders.X, layout.Messages.X, layout.Folders.Width+PanelGap)
	}
	if got, want := layout.Reader.X, layout.Messages.X+layout.Messages.Width+PanelGap; got != want {
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
	if got, want := layout.Folders.Width+layout.Messages.Width+layout.Reader.Width+2*PanelGap, ContentWidth(ThreeColumnBreakpoint); got != want {
		t.Errorf("column widths plus gaps sum to %d, want %d", got, want)
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
	for _, width := range []int{110, 115, 119} {
		layout := ComputeLayout(width, 30)

		if layout.Mode != ModeTwo {
			t.Fatalf("width %d: mode = %v, want ModeTwo", width, layout.Mode)
		}
		if !layout.Folders.Empty() {
			t.Errorf("width %d: folders box should be empty, got %+v", width, layout.Folders)
		}
		if got, want := layout.Messages.Width+layout.Reader.Width+PanelGap, ContentWidth(width); got != want {
			t.Errorf("width %d: columns plus gap sum to %d, want %d", width, got, want)
		}
		if layout.Messages.Width < MinMessagesWidth {
			t.Errorf("width %d: messages width = %d, want at least %d", width, layout.Messages.Width, MinMessagesWidth)
		}
		if layout.Reader.Width < 1 {
			t.Errorf("width %d: reader width = %d, want a positive width", width, layout.Reader.Width)
		}
	}
}

func TestSingleColumnThreshold(t *testing.T) {
	if got := ComputeLayout(NarrowBreakpoint-1, 30).Mode; got != ModeSingle {
		t.Errorf("mode at %d columns = %v, want ModeSingle", NarrowBreakpoint-1, got)
	}
	if got := ComputeLayout(NarrowBreakpoint, 30).Mode; got != ModeTwo {
		t.Errorf("mode at %d columns = %v, want ModeTwo", NarrowBreakpoint, got)
	}
}

func TestTabletWidthIsSinglePane(t *testing.T) {
	if got := ComputeLayout(105, 59).Mode; got != ModeSingle {
		t.Errorf("mode at the tablet width = %v, want ModeSingle", got)
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

// assertColumnsSum checks the invariant every layout must hold: the visible columns and
// the PanelGap columns between them add up to the content width, so no pane can overlap
// another or leave the frame short.
func assertColumnsSum(t *testing.T, layout Layout, width int) {
	t.Helper()

	sum := layout.Folders.Width + layout.Messages.Width + layout.Reader.Width + 2*PanelGap
	if layout.Mode == ModeTwo {
		sum = layout.Messages.Width + layout.Reader.Width + PanelGap
	}

	if want := ContentWidth(width); sum != want {
		t.Errorf("width %d: columns and gaps sum to %d, want %d (%+v)", width, sum, want, layout)
	}
}

// TestComputeLayoutWithColumnWidths proves an explicit column count is what the pane gets,
// rather than a proportion the user cannot predict.
func TestComputeLayoutWithColumnWidths(t *testing.T) {
	layout := ComputeLayoutWith(200, 40, Widths{FoldersWidth: 30, MessagesWidth: 70})

	if layout.Mode != ModeThree {
		t.Fatalf("mode = %v, want ModeThree", layout.Mode)
	}
	if got := layout.Folders.Width; got != 30 {
		t.Errorf("folders width = %d, want 30", got)
	}
	if got := layout.Messages.Width; got != 70 {
		t.Errorf("messages width = %d, want 70", got)
	}
	if got, want := layout.Reader.Width, ContentWidth(200)-100-2*PanelGap; got != want {
		t.Errorf("reader width = %d, want %d", got, want)
	}
	assertColumnsSum(t, layout, 200)
}

// TestComputeLayoutWithPercents proves a percentage is resolved against the content width,
// not the terminal width, so the three panes still add up.
func TestComputeLayoutWithPercents(t *testing.T) {
	layout := ComputeLayoutWith(200, 40, Widths{FoldersPercent: 20, MessagesPercent: 50})

	content := ContentWidth(200)
	if got, want := layout.Folders.Width, content*20/100; got != want {
		t.Errorf("folders width = %d, want %d", got, want)
	}
	if got, want := layout.Messages.Width, content*50/100; got != want {
		t.Errorf("messages width = %d, want %d", got, want)
	}
	assertColumnsSum(t, layout, 200)
}

// TestColumnWidthBeatsPercent proves the precedence the config documents: a column count
// wins over a percentage for the same pane.
func TestColumnWidthBeatsPercent(t *testing.T) {
	layout := ComputeLayoutWith(200, 40, Widths{
		FoldersPercent:  50,
		FoldersWidth:    26,
		MessagesPercent: 10,
		MessagesWidth:   55,
	})

	if got := layout.Folders.Width; got != 26 {
		t.Errorf("folders width = %d, want the configured 26 columns", got)
	}
	if got := layout.Messages.Width; got != 55 {
		t.Errorf("messages width = %d, want the configured 55 columns", got)
	}
	assertColumnsSum(t, layout, 200)
}

// TestReaderMinimumTrimsTheList proves the reader's 36 columns are not negotiable: a list
// width that would starve it is cut back instead of honoured.
func TestReaderMinimumTrimsTheList(t *testing.T) {
	layout := ComputeLayoutWith(120, 40, Widths{MessagesWidth: 150})

	want := ContentWidth(120) - layout.Folders.Width - 2*PanelGap - MinReaderWidth
	if got := layout.Messages.Width; got != want {
		t.Errorf("messages width = %d, want %d to leave the reader its minimum", got, want)
	}
	if got := layout.Reader.Width; got != MinReaderWidth {
		t.Errorf("reader width = %d, want %d", got, MinReaderWidth)
	}
	assertColumnsSum(t, layout, 120)
}

// TestReaderMinimumTrimsTheFoldersLast proves the order of sacrifice: the message list
// shrinks to its floor before the folder pane gives up a column.
func TestReaderMinimumTrimsTheFoldersLast(t *testing.T) {
	layout := ComputeLayoutWith(120, 40, Widths{FoldersWidth: 500, MessagesWidth: 500})

	if got := layout.Messages.Width; got != MinMessagesWidth {
		t.Errorf("messages width = %d, want the floor %d", got, MinMessagesWidth)
	}

	want := ContentWidth(120) - MinMessagesWidth - 2*PanelGap - MinReaderWidth
	if got := layout.Folders.Width; got != want {
		t.Errorf("folders width = %d, want %d, the remainder once the reader has its minimum", got, want)
	}
	if got := layout.Reader.Width; got != MinReaderWidth {
		t.Errorf("reader width = %d, want %d", got, MinReaderWidth)
	}
	assertColumnsSum(t, layout, 120)
}

// TestConfiguredWidthIsClampedToTheFloor proves a width too small to be usable is raised to
// the pane minimum rather than rendering a two-column list. config cannot reject it, because
// the same file is used at every terminal size.
func TestConfiguredWidthIsClampedToTheFloor(t *testing.T) {
	layout := ComputeLayoutWith(200, 40, Widths{FoldersWidth: 4, MessagesWidth: 10})

	if got := layout.Folders.Width; got != MinFoldersWidth {
		t.Errorf("folders width = %d, want the floor %d", got, MinFoldersWidth)
	}
	if got := layout.Messages.Width; got != MinMessagesWidth {
		t.Errorf("messages width = %d, want the floor %d", got, MinMessagesWidth)
	}
	assertColumnsSum(t, layout, 200)
}

// TestAutomaticMessagesWidthGrowsWithTheTerminal is the defect in #33: the list was pinned
// at 48 columns while a 200-column terminal gave the reader 118. The proportion now stands
// with only a floor under it, and the breakpoint is unchanged.
func TestAutomaticMessagesWidthGrowsWithTheTerminal(t *testing.T) {
	wide := ComputeLayout(200, 40)
	if got := wide.Messages.Width; got <= 48 {
		t.Errorf("messages width at 200 columns = %d, want more than the old 48-column cap", got)
	}

	atBreakpoint := ComputeLayout(ThreeColumnBreakpoint, 40)
	if got := atBreakpoint.Messages.Width; got != MinMessagesWidth {
		t.Errorf("messages width at %d columns = %d, want the floor %d",
			ThreeColumnBreakpoint, got, MinMessagesWidth)
	}
	if got := atBreakpoint.Folders.Width; got != 28 {
		t.Errorf("folders width at %d columns = %d, want the automatic 28", ThreeColumnBreakpoint, got)
	}
	if got := atBreakpoint.Reader.Width; got != 46-2*PanelGap {
		t.Errorf("reader width at %d columns = %d, want %d", ThreeColumnBreakpoint, got, 46-2*PanelGap)
	}
}

// TestTwoPaneHonoursMessagesWidth proves the message list is configurable in two-pane mode
// as well, where the folder pane is not drawn.
func TestTwoPaneHonoursMessagesWidth(t *testing.T) {
	layout := ComputeLayoutWith(115, 30, Widths{MessagesWidth: 50})

	if layout.Mode != ModeTwo {
		t.Fatalf("mode = %v, want ModeTwo", layout.Mode)
	}
	if got := layout.Messages.Width; got != 50 {
		t.Errorf("messages width = %d, want 50", got)
	}
	if got, want := layout.Reader.Width, ContentWidth(115)-50-PanelGap; got != want {
		t.Errorf("reader width = %d, want %d", got, want)
	}
	assertColumnsSum(t, layout, 115)
}

// TestZeroWidthsIsAutomatic proves the unconfigured install keeps the built-in rules, so a
// config without a [layout] section behaves exactly as before.
func TestZeroWidthsIsAutomatic(t *testing.T) {
	for _, width := range []int{120, 140, 200} {
		if got, want := ComputeLayoutWith(width, 40, Widths{}), ComputeLayout(width, 40); got != want {
			t.Errorf("width %d: ComputeLayoutWith(zero) = %+v, want %+v", width, got, want)
		}
	}
}

// TestSinglePaneIgnoresWidths proves a terminal too narrow for two panes is unaffected by
// sizing config, which has nothing to size there.
func TestSinglePaneIgnoresWidths(t *testing.T) {
	layout := ComputeLayoutWith(105, 59, Widths{FoldersWidth: 40, MessagesWidth: 60})

	if layout.Mode != ModeSingle {
		t.Fatalf("mode = %v, want ModeSingle", layout.Mode)
	}
	if got, want := layout.Reader.Width, ContentWidth(105); got != want {
		t.Errorf("reader width = %d, want %d", got, want)
	}
}
