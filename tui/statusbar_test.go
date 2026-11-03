package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/renderorange/chute/mail"
)

// statusBarModel opens INBOX with twelve unread messages and an optional status.
func statusBarModel(t *testing.T, width int, status string) Model {
	t.Helper()

	m := openInbox(t, withInbox(t, sampleMessages(12), nil))
	next, _ := m.Update(tea.WindowSizeMsg{Width: width, Height: 40})
	m = next.(Model)
	m.status = status

	return m
}

// TestStatusBarDropsContextBeforeTheStatus proves the first two context fields go when
// the status needs the room, and the whole status is still readable.
func TestStatusBarDropsContextBeforeTheStatus(t *testing.T) {
	m := statusBarModel(t, 60, "registration expired")
	bar := m.renderStatusBar()

	if !strings.Contains(bar, "registration expired") {
		t.Errorf("bar = %q, want the whole status", bar)
	}
	if strings.Contains(bar, "unread") {
		t.Errorf("bar = %q, still shows the unread count", bar)
	}
	if strings.Contains(bar, "messages") {
		t.Errorf("bar = %q, still shows the message count", bar)
	}
	if !strings.Contains(bar, "INBOX") {
		t.Errorf("bar = %q, dropped the folder too early", bar)
	}
	if !strings.HasPrefix(bar, "[fastmail]") {
		t.Errorf("bar = %q, want the account first", bar)
	}
	if got := lipgloss.Width(bar); got > ContentWidth(60) {
		t.Errorf("bar is %d cells, want at most %d", got, ContentWidth(60))
	}
}

// TestStatusBarDropsTheFolderBeforeTruncating proves the later context fields go too,
// and that no ellipsis is needed once they do.
func TestStatusBarDropsTheFolderBeforeTruncating(t *testing.T) {
	// ContentWidth(60) is 54: three cells of frame padding each side. Account and
	// separator cost ten plus three, so this status fills the rest exactly.
	status := strings.Repeat("y", ContentWidth(60)-len("[fastmail]")-len(" | "))
	m := statusBarModel(t, 60, status)
	bar := m.renderStatusBar()

	if !strings.Contains(bar, status) {
		t.Errorf("bar = %q, want the whole status", bar)
	}
	if strings.Contains(bar, "INBOX") || strings.Contains(bar, "folder") {
		t.Errorf("bar = %q, kept context that should have been dropped", bar)
	}
	if strings.Contains(bar, "...") {
		t.Errorf("bar = %q, truncated instead of dropping context", bar)
	}
	if got, want := lipgloss.Width(bar), ContentWidth(60); got != want {
		t.Errorf("bar is %d cells, want exactly %d", got, want)
	}
}

// TestStatusBarDropsTheConnectionStateToo proves a failed connection does not spend the
// width on "disconnected" when the status already says it.
func TestStatusBarDropsTheConnectionStateToo(t *testing.T) {
	m := statusBarModel(t, 60, "")
	next, _ := m.Update(foldersMsg{
		account: "fastmail",
		err:     fmt.Errorf("%w: account %q", mail.ErrConnect, "fastmail"),
	})
	m = next.(Model)

	bar := m.renderStatusBar()
	if !strings.Contains(bar, "cannot connect to the mail server") {
		t.Errorf("bar = %q, want the whole connection phrase", bar)
	}
	if strings.Contains(bar, "disconnected") {
		t.Errorf("bar = %q, kept the redundant state word", bar)
	}
	if got := lipgloss.Width(bar); got > ContentWidth(60) {
		t.Errorf("bar is %d cells, want at most %d", got, ContentWidth(60))
	}
}

// TestStatusBarTruncatesOnlyAfterDroppingEverything covers the last resort: a status
// wider than the whole bar is cut with an ellipsis rather than overflowing.
func TestStatusBarTruncatesOnlyAfterDroppingEverything(t *testing.T) {
	m := statusBarModel(t, 60, strings.Repeat("x", 200))
	bar := m.renderStatusBar()

	if got, want := lipgloss.Width(bar), ContentWidth(60); got != want {
		t.Errorf("bar is %d cells, want exactly %d", got, want)
	}
	if !strings.Contains(bar, "...") {
		t.Errorf("bar = %q, want an ellipsis", bar)
	}
	if !strings.HasPrefix(bar, "[fastmail] | xxx") {
		t.Errorf("bar = %q, want the account and the start of the status", bar)
	}
}

// TestStatusBarKeepsContextWithoutAStatus proves the packing only applies when there is
// a status to make room for.
func TestStatusBarKeepsContextWithoutAStatus(t *testing.T) {
	m := statusBarModel(t, 105, "")
	bar := m.renderStatusBar()

	for _, want := range []string{"1 folder", "INBOX", "12 messages", "12 unread"} {
		if !strings.Contains(bar, want) {
			t.Errorf("bar = %q, missing %q", bar, want)
		}
	}
}

// TestStatusBarDoesNotRepeatTheActiveAccount proves the wrapped prefix is dropped from an
// unknown error's displayed text, while another account's name is left alone.
func TestStatusBarDoesNotRepeatTheActiveAccount(t *testing.T) {
	m := statusBarModel(t, 105, `mail: account "fastmail": opening "INBOX": NO such mailbox`)
	bar := m.renderStatusBar()

	if strings.Contains(bar, `account "fastmail"`) {
		t.Errorf("bar = %q, repeats the account the bar already shows", bar)
	}
	if !strings.Contains(bar, `opening "INBOX"`) {
		t.Errorf("bar = %q, dropped the useful part of the error", bar)
	}

	other := statusBarModel(t, 105, `mail: account "work": opening "INBOX": refused`)
	if got := other.renderStatusBar(); !strings.Contains(got, `account "work"`) {
		t.Errorf("bar = %q, hid a different account named in the error", got)
	}
}

// TestStatusBarFitsTheFrameAtEveryWidth is the issue's width criterion, measured on the
// whole view so the bar and the frame are checked together.
func TestStatusBarFitsTheFrameAtEveryWidth(t *testing.T) {
	for _, width := range []int{60, 105, 130} {
		m := statusBarModel(t, width, strings.Repeat("x", 200))

		lines := strings.Split(m.View(), "\n")
		if len(lines) != 40 {
			t.Errorf("%d columns: view has %d lines, want 40", width, len(lines))
		}
		for i, line := range lines {
			if got := lipgloss.Width(line); got != width {
				t.Errorf("%d columns: line %d is %d cells: %q", width, i, got, line)
				break
			}
		}
	}
}

// TestStatusBarTruncatesWideRunes proves the bar counts display cells, not runes, so a
// wide-rune folder or status cannot overflow the frame.
func TestStatusBarTruncatesWideRunes(t *testing.T) {
	m := statusBarModel(t, 60, strings.Repeat("日本語", 20))

	for i, line := range strings.Split(m.View(), "\n") {
		if got := lipgloss.Width(line); got != 60 {
			t.Fatalf("line %d is %d cells, want 60: %q", i, got, line)
		}
	}
}
