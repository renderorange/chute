package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/emersion/go-imap/v2"
	"github.com/renderorange/chute/mail"
)

// TestRenderPanelFillsBox pins the panel geometry. lipgloss counts horizontal
// padding as part of Width, so a panel sized to its inner width word-wraps every
// full-width row and comes out two cells too narrow.
func TestRenderPanelFillsBox(t *testing.T) {
	m := sized(105, 59)
	box := Box{Width: 40, Height: 10}
	row := strings.Repeat("x", box.Width-(2*PanelPaddingHorizontal))

	panel := m.renderPanel("messages", box, []string{row}, true)

	if got := lipgloss.Width(panel); got != box.Width {
		t.Errorf("panel width = %d, want %d", got, box.Width)
	}
	if got := lipgloss.Height(panel); got != box.Height {
		t.Errorf("panel height = %d, want %d", got, box.Height)
	}

	found := false
	for _, line := range strings.Split(panel, "\n") {
		if strings.Contains(line, row) {
			found = true
		}
	}
	if !found {
		t.Errorf("the full-width row wrapped instead of filling one line:\n%s", panel)
	}
}

// openFirstFolder opens the folder under the folder cursor. Unlike the enter key
// this works in every layout mode, including those where the folders panel is not
// drawn.
func openFirstFolder(t *testing.T, m Model) Model {
	t.Helper()

	m.folderCursor = 0
	next, cmd := m.openSelectedFolder()
	m = next.(Model)
	if cmd == nil {
		t.Fatal("opening the folder returned no command")
	}

	next, _ = m.Update(cmd())

	return next.(Model)
}

// TestMessageDateStaysOnItsRow is the tablet regression: the list row is exactly
// the panel's inner width, and a row two cells too wide word-wrapped the day
// number onto a second line. "Oct 30" must stay on the row, at its end.
func TestMessageDateStaysOnItsRow(t *testing.T) {
	date := time.Date(2026, 10, 30, 12, 0, 0, 0, time.Local)

	// Unread and flagged change the marker cells, which is what used to make the
	// row overflow.
	states := []struct {
		name string
		// seen and flagged go through the envelope flags.
		seen, flagged bool
	}{
		{"read", true, false},
		{"unread", false, false},
		{"flagged", true, true},
		{"unread and flagged", false, true},
	}

	widths := []int{105, 115, 130}

	for _, width := range widths {
		for _, state := range states {
			t.Run(fmt.Sprintf("%d/%s", width, state.name), func(t *testing.T) {
				var flags []imap.Flag
				if state.seen {
					flags = append(flags, imap.FlagSeen)
				}
				if state.flagged {
					flags = append(flags, imap.FlagFlagged)
				}

				m := loaded(t, width, 59, []mail.Folder{{Name: "INBOX", Delimiter: '/'}}, nil)
				m.loadMessages = fakeMessages([]mail.Message{{
					UID: 1,
					Envelope: imap.Envelope{
						Subject: "a long subject that fills the wide single panel list",
						From:    []imap.Address{{Name: "Blaine", Mailbox: "blaine", Host: "example.org"}},
						Date:    date,
					},
					Flags: flags,
				}}, nil)
				m = openFirstFolder(t, m)

				view := m.View()
				var row string
				for _, line := range strings.Split(view, "\n") {
					if strings.TrimSpace(line) == "30" || strings.TrimSpace(line) == "Oct" {
						t.Fatalf("the date wrapped onto its own line:\n%s", view)
					}
					if strings.Contains(line, "Oct 30") {
						row = line
					}
				}

				if row == "" {
					t.Fatalf("no rendered line held the date:\n%s", view)
				}
				if !strings.Contains(row, "a long subject") {
					t.Errorf("date row %q lost the subject", row)
				}

				// The row itself must be one cell-exact line ending in the date, so
				// nothing can wrap when the panel renders it.
				layout := ComputeLayout(width, 59)
				box := layout.Messages
				if box.Empty() {
					box = layout.Reader
				}
				rowWidth := box.Width - (2 * PanelPaddingHorizontal)

				line := m.messageLine(0, rowWidth)
				if got := lipgloss.Width(line); got != rowWidth {
					t.Errorf("message row is %d cells, want %d", got, rowWidth)
				}
				if got := lipgloss.Height(line); got != 1 {
					t.Errorf("message row is %d lines, want 1", got)
				}
				if !strings.HasSuffix(strings.TrimRight(line, " "), "Oct 30") {
					t.Errorf("message row %q does not end with the date", line)
				}
			})
		}
	}
}

// TestMessageRowsExactWidthWithIndent pins that indentation comes out of the
// subject budget instead of being added to the row: every depth stays exactly the
// panel's inner width and keeps its date, and two cells of indent appear per level
// up to the cap.
func TestMessageRowsExactWidthWithIndent(t *testing.T) {
	const rowWidth = 38

	messages := []mail.Message{threadMessage(1, "subject", "<m1>", 30)}
	for i := 2; i <= 6; i++ {
		messages = append(messages, threadMessage(
			imap.UID(i),
			"subject",
			fmt.Sprintf("<m%d>", i),
			30,
			fmt.Sprintf("<m%d>", i-1),
		))
	}

	m := loaded(t, 105, 59, []mail.Folder{{Name: "INBOX", Delimiter: '/'}}, nil)
	m.messages = messages
	m.rows = BuildRows(messages)

	if len(m.rows) != 6 {
		t.Fatalf("rows = %d, want 6", len(m.rows))
	}

	base := strings.Index(m.messageLine(0, rowWidth), "subject")
	if base < 0 {
		t.Fatalf("the first row does not contain the subject")
	}

	for index, row := range m.rows {
		line := m.messageLine(index, rowWidth)

		if got := lipgloss.Width(line); got != rowWidth {
			t.Errorf("row %d at depth %d is %d cells, want %d", index, row.Depth, got, rowWidth)
		}
		if got := lipgloss.Height(line); got != 1 {
			t.Errorf("row %d at depth %d is %d lines, want 1", index, row.Depth, got)
		}
		if !strings.HasSuffix(strings.TrimRight(line, " "), "Oct 30") {
			t.Errorf("row %d at depth %d does not end with the date: %q", index, row.Depth, line)
		}

		wantStart := base + (2 * row.Depth)
		if got := strings.Index(line, "subject"); got != wantStart {
			t.Errorf("row %d subject starts at %d, want %d for depth %d", index, got, wantStart, row.Depth)
		}
		if want := min(index, MaxThreadDepth); row.Depth != want {
			t.Errorf("row %d depth = %d, want %d", index, row.Depth, want)
		}
	}
}

// TestFooterHintsReader proves the reader names the thread keys, and that they
// survive the width the tablet actually has rather than being truncated away.
func TestFooterHintsReader(t *testing.T) {
	m := readerThreadModel(t)
	m.width = 105
	m.height = 59

	footer := m.renderFooter()

	for _, want := range []string{"n:next", "p:prev"} {
		if !strings.Contains(footer, want) {
			t.Errorf("reader footer missing %q: %q", want, footer)
		}
	}
	if got, limit := lipgloss.Width(footer), ContentWidth(105); got > limit {
		t.Errorf("footer is %d cells, want at most %d", got, limit)
	}
}

// TestChromeGaps pins the bottom block: a blank line separates the panels from the
// footer, and another separates the footer from the status bar.
func TestChromeGaps(t *testing.T) {
	m := loaded(t, 105, 59, []mail.Folder{{Name: "INBOX", Delimiter: '/'}}, nil)
	lines := strings.Split(m.View(), "\n")

	if got := len(lines); got != m.height {
		t.Fatalf("view has %d lines, want %d", got, m.height)
	}

	if got := strings.TrimSpace(lines[len(lines)-5]); got != "" {
		t.Errorf("line above the footer = %q, want blank", got)
	}
	if footer := lines[len(lines)-4]; !strings.Contains(footer, "q:quit") {
		t.Errorf("footer line = %q, want the key hints", footer)
	}
	if got := strings.TrimSpace(lines[len(lines)-3]); got != "" {
		t.Errorf("line above the status bar = %q, want blank", got)
	}
	if status := lines[len(lines)-2]; !strings.Contains(status, "[fastmail]") {
		t.Errorf("status line = %q, want the account", status)
	}
}

// TestViewFillsTerminalWidth catches any panel that fails to consume its box:
// every rendered line must be exactly the terminal width.
func TestViewFillsTerminalWidth(t *testing.T) {
	for _, width := range []int{105, 115, 130} {
		view := loaded(t, width, 40, sampleFolders(3), nil).View()

		for i, line := range strings.Split(view, "\n") {
			if got := lipgloss.Width(line); got != width {
				t.Errorf("width %d: line %d is %d cells, want %d", width, i, got, width)
				break
			}
		}
	}
}

// titleLine returns the first rendered content line, where the panel titles sit. The
// frame's padding occupies the lines above it.
func titleLine(view string) string {
	return firstContentLine(view)
}

// markedPanels reports which panel titles carry the focus marker on a line.
func markedPanels(line string) []string {
	var marked []string
	for _, name := range []string{"folders", "messages", "reader", "compose"} {
		if strings.Contains(line, "> "+name) {
			marked = append(marked, name)
		}
	}

	return marked
}

// TestExactlyOnePanelShowsFocus proves the marker exists, is unique, and is visible in
// plain text at every layout width. Without it, a user pressing m in the folder panel
// sees nothing happen and no reason why.
func TestExactlyOnePanelShowsFocus(t *testing.T) {
	for _, width := range []int{140, 115, 105} {
		m := sized(width, 40)

		marked := markedPanels(titleLine(m.View()))
		if len(marked) != 1 {
			t.Errorf("width %d: %d panels marked, want exactly 1: %v", width, len(marked), marked)
		}
	}
}

// TestTabMovesTheFocusMarker proves the marker follows focus rather than being decorative.
func TestTabMovesTheFocusMarker(t *testing.T) {
	m := sized(140, 40)

	before := markedPanels(titleLine(m.View()))
	if len(before) != 1 {
		t.Fatalf("start: marked %v", before)
	}

	for i := 0; i < 3; i++ {
		next, _ := m.Update(tea.KeyMsg{Type: tea.KeyTab})
		m = next.(Model)

		marked := markedPanels(titleLine(m.View()))
		if len(marked) != 1 {
			t.Fatalf("after tab %d: %d panels marked: %v", i+1, len(marked), marked)
		}
		if marked[0] == before[0] {
			t.Errorf("after tab %d: marker stayed on %s", i+1, marked[0])
		}

		before = marked
	}
}

// TestComposerShowsFocus proves the composer counts as the focused panel while it is open.
func TestComposerShowsFocus(t *testing.T) {
	m, _ := withComposeModel(t)
	m = press(t, m, keyMsg("r"))

	marked := markedPanels(titleLine(m.View()))
	if len(marked) != 1 || marked[0] != "compose" {
		t.Errorf("marked %v, want only compose", marked)
	}
}

// TestViewAtTheMinimumSize renders 60x20, the floor the design promises and that the M6
// gate names. layout_test.go proves the arithmetic; this proves the frame composes at it:
// every line fills the terminal, the focused panel shows its body, and the footer and
// status bar are both there.
func TestViewAtTheMinimumSize(t *testing.T) {
	const width, height = 60, 20

	// The numbers the gate is stated in: 54 content cells and 14 content rows.
	if got := ContentWidth(width); got != 54 {
		t.Fatalf("ContentWidth(%d) = %d, want 54", width, got)
	}
	if got := ContentHeight(height); got != 14 {
		t.Fatalf("ContentHeight(%d) = %d, want 14", height, got)
	}

	for _, tc := range []struct {
		focus   panelFocus
		title   string
		content string
		hint    string
	}{
		{focusFolders, "folders", "INBOX", "q:quit"},
		{focusMessages, "messages", "subject 03", "q:quit"},
		{focusReader, "reader", "subject 03", "n:next"},
	} {
		m := loaded(t, width, height, sampleFolders(3), nil)
		m.loadMessages = fakeMessages(sampleMessages(3), nil)
		m = openInbox(t, m)
		m.focus = tc.focus

		view := m.View()
		lines := strings.Split(view, "\n")

		if len(lines) != height {
			t.Fatalf("%s: %d lines, want %d", tc.title, len(lines), height)
		}
		for i, line := range lines {
			if got := lipgloss.Width(line); got != width {
				t.Fatalf("%s: line %d is %d cells, want %d: %q", tc.title, i, got, width, line)
			}
		}

		if got := titleLine(view); !strings.Contains(got, tc.title) {
			t.Errorf("%s: the panel title is not on the title line: %q", tc.title, got)
		}
		if !strings.Contains(view, tc.content) {
			t.Errorf("%s: the panel body is empty, missing %q:\n%s", tc.title, tc.content, view)
		}
		if footer := lines[len(lines)-4]; !strings.Contains(footer, tc.hint) {
			t.Errorf("%s: the footer is missing %q: %q", tc.title, tc.hint, footer)
		}
		if status := lines[len(lines)-2]; !strings.Contains(status, "[fastmail]") {
			t.Errorf("%s: the status bar is missing: %q", tc.title, status)
		}
	}
}

// TestViewAtEachBreakpointHasNoEmptyPanel proves the two-pane and three-pane layouts at
// exactly their breakpoints draw every panel they claim to, so neither mode has a box
// that collapses to zero at the width that selects it.
func TestViewAtEachBreakpointHasNoEmptyPanel(t *testing.T) {
	for _, tc := range []struct {
		width  int
		mode   Mode
		titles []string
	}{
		{NarrowBreakpoint, ModeTwo, []string{"messages", "reader"}},
		{ThreeColumnBreakpoint, ModeThree, []string{"folders", "messages", "reader"}},
	} {
		layout := ComputeLayout(tc.width, 20)
		if layout.Mode != tc.mode {
			t.Fatalf("width %d chose mode %v, want %v", tc.width, layout.Mode, tc.mode)
		}

		visible := map[string]Box{"messages": layout.Messages, "reader": layout.Reader}
		if tc.mode == ModeThree {
			visible["folders"] = layout.Folders
		} else if !layout.Folders.Empty() {
			t.Errorf("width %d: the folders box is drawn in two-pane mode: %+v", tc.width, layout.Folders)
		}

		for name, box := range visible {
			if box.Empty() {
				t.Errorf("width %d (%v): the %s box is empty: %+v", tc.width, tc.mode, name, box)
			}
		}

		view := loaded(t, tc.width, 20, sampleFolders(3), nil).View()
		for _, title := range tc.titles {
			if got := titleLine(view); !strings.Contains(got, title) {
				t.Errorf("width %d: %q is missing from the title line: %q", tc.width, title, got)
			}
		}
		for i, line := range strings.Split(view, "\n") {
			if got := lipgloss.Width(line); got != tc.width {
				t.Fatalf("width %d: line %d is %d cells: %q", tc.width, i, got, line)
			}
		}
	}
}
