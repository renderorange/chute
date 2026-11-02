package tui

import (
	"regexp"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/emersion/go-imap/v2"
	"github.com/muesli/termenv"
)

// errorForeground is the SGR sequence ErrorStyle emits for the palette's error
// colour (167). Asserting on the sequence, not on the style value, is what makes
// the test fail if the style is swapped for a heading style.
const errorForeground = "\x1b[38;5;167m"

// styledModal renders the delete confirmation with colour output forced on, so
// the assertions are not vacuous under `go test`'s piped stdout. The profile is
// restored before the test returns; no test in this package uses t.Parallel.
func styledModal(t *testing.T, m Model) string {
	t.Helper()

	profile := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(profile) })

	return m.renderConfirmModal(ContentWidth(m.width), ContentHeight(m.height))
}

// deleteConfirmModel is a model with a pending delete and the dialog open.
func deleteConfirmModel(width int) Model {
	m := sized(width, 40)
	m.confirmOpen = true
	m.confirmKind = confirmDelete
	m.pendingDelete = []imap.UID{7}

	return m
}

// discardConfirmModel is a model asking whether to discard a draft.
func discardConfirmModel(width int) Model {
	m := sized(width, 40)
	m.confirmOpen = true
	m.confirmKind = confirmDiscardDraft
	m.compose = &composer{}

	return m
}

var ansiSequence = regexp.MustCompile("\x1b\\[[0-9;]*m")

// TestConfirmTitleCarriesTheErrorColour is the defect in #26: a destructive
// confirmation must not read as a panel heading. The title wears the error
// foreground; the question and the key hints do not, or the whole dialog shouts.
func TestConfirmTitleCarriesTheErrorColour(t *testing.T) {
	modal := styledModal(t, deleteConfirmModel(105))

	if !strings.Contains(modal, errorForeground+"delete") {
		t.Errorf("the delete title is not in the error colour: %q", modal)
	}
	if !strings.Contains(modal, "move 1 message to trash?") {
		t.Errorf("the question is missing: %q", modal)
	}
	if got := strings.Contains(modal, errorForeground+"move 1 message to trash?"); got {
		t.Errorf("the question wears the error colour, which makes it shout: %q", modal)
	}
	if got := strings.Contains(modal, errorForeground+"enter:delete"); got {
		t.Errorf("the hints wear the error colour: %q", modal)
	}
}

// TestDiscardTitleCarriesTheErrorColour covers the second confirmation: discarding
// a draft loses work, so it is as destructive as a delete.
func TestDiscardTitleCarriesTheErrorColour(t *testing.T) {
	modal := styledModal(t, discardConfirmModel(105))

	if !strings.Contains(modal, errorForeground+"discard") {
		t.Errorf("the discard title is not in the error colour: %q", modal)
	}
	if strings.Contains(modal, errorForeground+"discard this message?") {
		t.Errorf("the question wears the error colour: %q", modal)
	}
	if strings.Contains(modal, errorForeground+"enter:discard") {
		t.Errorf("the hints wear the error colour: %q", modal)
	}
}

// TestConfirmIsReadableWithoutColour proves colour only reinforces the signal:
// with every escape stripped, the title, the question and the key hint are three
// distinguishable things.
func TestConfirmIsReadableWithoutColour(t *testing.T) {
	modal := styledModal(t, deleteConfirmModel(105))
	plain := ansiSequence.ReplaceAllString(modal, "")

	lines := map[string]bool{}
	for _, line := range strings.Split(plain, "\n") {
		lines[strings.Trim(strings.TrimSpace(line), "│ ")] = true
	}

	for _, want := range []string{"delete", "move 1 message to trash?", "enter:delete  esc:cancel"} {
		if !lines[want] {
			t.Errorf("the monochrome dialog is missing the line %q:\n%s", want, plain)
		}
	}
}

// TestConfirmTitleIsNotThePanelTitle is the "not a heading" half of the issue:
// the title must not be rendered with TitleStyle.
func TestConfirmTitleIsNotThePanelTitle(t *testing.T) {
	modal := styledModal(t, deleteConfirmModel(105))

	if strings.Contains(modal, TitleStyle.Render("delete")) {
		t.Errorf("the delete title is still rendered as a panel title: %q", modal)
	}
}

// TestConfirmGeometryIsUnchanged guards the issue's "rest of the modal is
// unchanged" criterion: one rounded border, centred horizontally, same width
// regardless of which confirmation is open.
func TestConfirmGeometryIsUnchanged(t *testing.T) {
	width, height := ContentWidth(105), ContentHeight(40)

	del := deleteConfirmModel(105).renderConfirmModal(width, height)
	disc := discardConfirmModel(105).renderConfirmModal(width, height)

	if got := lipgloss.Width(del); got != width {
		t.Errorf("delete modal is %d cells wide, want %d", got, width)
	}
	if got := lipgloss.Width(disc); got != width {
		t.Errorf("discard modal is %d cells wide, want %d", got, width)
	}
	if got := lipgloss.Height(del); got != height {
		t.Errorf("delete modal is %d lines, want %d", got, height)
	}

	for _, modal := range []string{del, disc} {
		if !strings.Contains(modal, "╭") || !strings.Contains(modal, "╰") {
			t.Errorf("modal lost its rounded border:\n%s", modal)
		}
	}
}
