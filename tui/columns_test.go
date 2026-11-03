package tui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/emersion/go-imap/v2"
	"github.com/renderorange/chute/config"
	"github.com/renderorange/chute/mail"
)

// listModel builds a model whose message list holds one fixed message, so a row can
// be rendered at any width against a [list] config without a server or a folder.
func listModel(cols []string, format string) Model {
	cfg := testConfig()
	cfg.List.Columns = cols
	cfg.List.DateFormat = format

	m := sizedWith(cfg, 140, 40)
	m.messages = []mail.Message{{
		UID: 1,
		Envelope: imap.Envelope{
			Subject: "subject 01",
			From:    []imap.Address{{Name: "Sender", Mailbox: "s", Host: "example.org"}},
			Date:    time.Date(2026, 10, 29, 12, 0, 0, 0, time.Local),
		},
	}}
	m.rows = BuildRows(m.messages)

	return m
}

// TestListColumnsFollowTheConfigOrder proves "order" is the list order: the date
// comes before the sender when the config says so.
func TestListColumnsFollowTheConfigOrder(t *testing.T) {
	m := listModel([]string{config.ListColumnDate, config.ListColumnFrom, config.ListColumnSubject}, "")

	line := m.messageLine(0, 52)
	date := strings.Index(line, "Oct 29")
	sender := strings.Index(line, "Sender")

	if date < 0 || sender < 0 {
		t.Fatalf("row is missing a column: %q", line)
	}
	if date > sender {
		t.Errorf("date at %d is after the sender at %d, want the configured order: %q", date, sender, line)
	}
}

// TestListColumnsCanHideTheDate proves omitting the column is how the date is
// removed: no leftover gap, and the row still fills the pane exactly.
func TestListColumnsCanHideTheDate(t *testing.T) {
	m := listModel([]string{config.ListColumnFrom, config.ListColumnSubject}, "")

	for _, width := range []int{38, 52, 97} {
		line := m.messageLine(0, width)

		if strings.Contains(line, "Oct 29") {
			t.Errorf("row at %d still shows the date: %q", width, line)
		}
		if got := lipgloss.Width(line); got != width {
			t.Errorf("row at %d is %d cells, want exactly %d", width, got, width)
		}
	}
}

// TestListDateFormatIsConfigurable is the issue's third knob.
func TestListDateFormatIsConfigurable(t *testing.T) {
	m := listModel(nil, "2006-01-02")

	if line := m.messageLine(0, 52); !strings.Contains(line, "2026-10-29") {
		t.Errorf("row does not use the configured date format: %q", line)
	}
}

// TestMessageLineWidthWithCustomColumns proves the row arithmetic generalizes: any
// order, any width, exactly one line of exactly that width.
func TestMessageLineWidthWithCustomColumns(t *testing.T) {
	orders := [][]string{
		nil, // the shipped default
		{config.ListColumnDate, config.ListColumnFrom, config.ListColumnSubject},
		{config.ListColumnSubject, config.ListColumnFrom, config.ListColumnDate},
		{config.ListColumnFrom, config.ListColumnSubject},
		{config.ListColumnFrom, config.ListColumnSubject, config.ListColumnSize, config.ListColumnDate},
	}

	for _, order := range orders {
		m := listModel(order, "")

		for _, width := range []int{38, 44, 52, 97, 200} {
			line := m.messageLine(0, width)

			if got := lipgloss.Width(line); got != width {
				t.Errorf("order %v at %d: width = %d, want %d: %q", order, width, got, width, line)
			}
			if got := lipgloss.Height(line); got != 1 {
				t.Errorf("order %v at %d: height = %d, want 1", order, width, got)
			}
		}
	}
}

// TestMessageLineSurvivesAWideDateLiteral is the frame invariant for user input: a
// format full of wide runes cannot widen the row or wrap it, because the date is
// measured and truncated in cells.
func TestMessageLineSurvivesAWideDateLiteral(t *testing.T) {
	m := listModel(nil, "年01月02日年01月02日年01月02日年01月02日")

	for _, width := range []int{38, 52, 97} {
		line := m.messageLine(0, width)

		if got := lipgloss.Width(line); got > width {
			t.Errorf("row at %d is %d cells, want at most %d: %q", width, got, width, line)
		}
		if got := lipgloss.Height(line); got != 1 {
			t.Errorf("row at %d has %d lines, want 1", width, got)
		}
	}
}

// sizeModel is listModel with one message of a given size, so the size column can be
// rendered against the real row builder.
func sizeModel(size int64, cols []string) Model {
	m := listModel(cols, "")
	m.messages[0].Size = size
	m.rows = BuildRows(m.messages)

	return m
}

// TestSizeColumnRendersTheByteCount is #63's acceptance list: the five sizes the issue
// names, rendered where the config puts them.
func TestSizeColumnRendersTheByteCount(t *testing.T) {
	cols := []string{config.ListColumnFrom, config.ListColumnSubject, config.ListColumnSize, config.ListColumnDate}

	for _, tc := range []struct {
		name string
		size int64
		want string
	}{
		{"empty", 0, "0B"},
		{"bytes", 999, "999B"},
		{"kilobytes", 1536, "1.5K"},
		{"megabytes", 2 * 1024 * 1024, "2.0M"},
		{"gigabytes", 1288490188, "1.2G"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			line := sizeModel(tc.size, cols).messageLine(0, 60)

			if !strings.Contains(line, tc.want) {
				t.Errorf("row is missing %q: %q", tc.want, line)
			}
			if got := lipgloss.Width(line); got != 60 {
				t.Errorf("row is %d cells, want 60: %q", got, line)
			}
		})
	}
}

// TestSizeColumnWorksWithoutTheDate proves the size column does not depend on the date
// being present, which is how a narrow layout would use it.
func TestSizeColumnWorksWithoutTheDate(t *testing.T) {
	line := sizeModel(1536, []string{config.ListColumnFrom, config.ListColumnSubject, config.ListColumnSize}).messageLine(0, 44)

	if !strings.Contains(line, "1.5K") {
		t.Errorf("row is missing the size: %q", line)
	}
	if got := lipgloss.Width(line); got != 44 {
		t.Errorf("row is %d cells, want 44: %q", got, line)
	}
}

// TestSizeCellIsRightAlignedAndCapped pins the cell itself: fixed width, value flush
// right, and a value past the cap loses leading digits rather than widening the row.
func TestSizeCellIsRightAlignedAndCapped(t *testing.T) {
	for _, tc := range []struct {
		size int64
		want string
	}{
		{0, "    0B"},
		{999, "  999B"},
		{1536, "  1.5K"},
	} {
		if got := sizeCell(tc.size); got != tc.want {
			t.Errorf("sizeCell(%d) = %q, want %q", tc.size, got, tc.want)
		}
	}

	if got := lipgloss.Width(sizeCell(1 << 50)); got != config.ListMaxSizeCells {
		t.Errorf("an oversized value renders %d cells, want %d", got, config.ListMaxSizeCells)
	}
}

// TestMessageLineDefaultColumnsAreUnchanged pins the shipped row's shape, so the
// generalization cannot quietly move the sender, the gap or the date.
func TestMessageLineDefaultColumnsAreUnchanged(t *testing.T) {
	m := listModel(nil, "")
	line := m.messageLine(0, 52)

	if got := lipgloss.Width(line); got != 52 {
		t.Fatalf("width = %d, want 52: %q", got, line)
	}
	if !strings.HasPrefix(line, ">  Sender") {
		t.Errorf("row does not start with the markers and the sender: %q", line)
	}
	if !strings.HasSuffix(line, "Oct 29") {
		t.Errorf("row does not end with the date: %q", line)
	}
	if !strings.Contains(line, "   Oct 29") {
		t.Errorf("row lost the three-cell gap before the date: %q", line)
	}

	// Exact shape: 3 marker cells, the sender clamped to 53/4=13, a 1-cell gap, the
	// subject taking the rest at 26, the 3-cell date gap and the 6-cell date.
	want := ">  " + "Sender" + strings.Repeat(" ", 7) + " " +
		"subject 01" + strings.Repeat(" ", 16) + "   " + "Oct 29"
	if line != want {
		t.Errorf("shipped row changed:\ngot  %q\nwant %q", line, want)
	}
}

// TestFrameAtTheTabletSizeWithEachListConfig is the whole-frame invariant at the
// owner's terminal: whatever [list] says, every line is still exactly 105 cells and
// the frame is still exactly 59 lines.
func TestFrameAtTheTabletSizeWithEachListConfig(t *testing.T) {
	for _, tc := range []struct {
		name string
		list config.List
	}{
		{name: "default"},
		{name: "date first", list: config.List{Columns: []string{config.ListColumnDate, config.ListColumnFrom, config.ListColumnSubject}}},
		{name: "no date", list: config.List{Columns: []string{config.ListColumnFrom, config.ListColumnSubject}}},
		{name: "size", list: config.List{Columns: []string{config.ListColumnFrom, config.ListColumnSubject, config.ListColumnSize, config.ListColumnDate}}},
		{name: "iso date", list: config.List{DateFormat: "2006-01-02"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := testConfig()
			cfg.List = tc.list

			m := loadedWith(t, cfg, []mail.Folder{{Name: "INBOX", Delimiter: '/'}})
			m.loadMessages = fakeMessages(sampleMessages(3), nil)
			m = openInbox(t, m)

			next, _ := m.Update(tea.WindowSizeMsg{Width: 105, Height: 59})
			lines := strings.Split(next.(Model).View(), "\n")

			if len(lines) != 59 {
				t.Fatalf("view has %d lines, want 59", len(lines))
			}
			for i, line := range lines {
				if got := lipgloss.Width(line); got != 105 {
					t.Fatalf("line %d is %d cells, want 105: %q", i, got, line)
				}
			}
		})
	}
}
