package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
	"github.com/renderorange/chute/config"
	"github.com/renderorange/chute/mail"
)

// treeFolders is a listing two levels deep: Work, a year under it, and a quarter under
// the year. INBOX is a leaf, so it must never grow a marker.
func treeFolders() []mail.Folder {
	return []mail.Folder{
		{Name: "INBOX", Delimiter: '/'},
		{Name: "Work", Delimiter: '/'},
		{Name: "Work/2024", Delimiter: '/'},
		{Name: "Work/2024/Q1", Delimiter: '/'},
		{Name: "Work/2025", Delimiter: '/'},
	}
}

// folderPaneText renders more rows than any fixture has, so a hidden row cannot pass by
// being scrolled off instead of folded.
func folderPaneText(m Model) string {
	return strings.Join(m.folderLines(20, 24), "\n")
}

// updateNamed presses a named key (enter, esc, left, right) and returns the model.
func updateNamed(t *testing.T, m Model, key string) Model {
	t.Helper()

	next, _ := m.Update(namedKey(key))

	return next.(Model)
}

// TestFolderTreeCollapsesByDefault is the issue's default: a new install shows the top
// level, and a folder with subfolders advertises them with a "+".
func TestFolderTreeCollapsesByDefault(t *testing.T) {
	m := loaded(t, 140, 40, treeFolders(), nil)

	text := folderPaneText(m)
	for _, hidden := range []string{"2024", "Q1", "2025"} {
		if strings.Contains(text, hidden) {
			t.Errorf("pane shows %q while the tree is collapsed:\n%s", hidden, text)
		}
	}
	if !strings.Contains(text, "+ Work") {
		t.Errorf("Work has no + marker:\n%s", text)
	}
	if strings.Contains(text, "+ INBOX") {
		t.Errorf("INBOX has subfolders it does not have:\n%s", text)
	}
	if got, want := len(m.folderTree()), 2; got != want {
		t.Errorf("visible rows = %d, want %d", got, want)
	}
}

// TestEnterExpandsThenOpens pins the owner's ruling: enter reveals a collapsed folder
// first, and the next enter behaves exactly as it always has and loads the folder.
func TestEnterExpandsThenOpens(t *testing.T) {
	m := loaded(t, 140, 40, treeFolders(), nil)
	m.folderCursor = 1 // Work

	m = updateNamed(t, m, "enter")

	if m.openFolder != "" {
		t.Errorf("enter opened %q, want the first press to expand only", m.openFolder)
	}
	if !m.folderExpanded["Work"] {
		t.Error("Work is not expanded after enter")
	}
	if text := folderPaneText(m); !strings.Contains(text, "- Work") || !strings.Contains(text, "2024") {
		t.Errorf("pane does not show the markers and children of an expanded folder:\n%s", text)
	}

	m = updateNamed(t, m, "enter")

	if got, want := m.openFolder, "Work"; got != want {
		t.Errorf("openFolder = %q, want %q on the second enter", got, want)
	}
}

// TestEnterOpensALeaf proves the two-step does not delay a folder that has nothing to
// reveal: the first press still opens it.
func TestEnterOpensALeaf(t *testing.T) {
	m := loaded(t, 140, 40, treeFolders(), nil)
	m.folderCursor = 0 // INBOX

	m = updateNamed(t, m, "enter")

	if got, want := m.openFolder, "INBOX"; got != want {
		t.Errorf("openFolder = %q, want %q", got, want)
	}
}

// TestArrowKeysExpandAndFoldTheFolder is the tree gesture: right reveals the subfolders,
// left folds them, and neither enters the folder. Enter keeps its own meaning.
func TestArrowKeysExpandAndFoldTheFolder(t *testing.T) {
	m := loaded(t, 140, 40, treeFolders(), nil)
	m.folderCursor = 1 // Work

	m = updateNamed(t, m, "right")

	if m.openFolder != "" {
		t.Errorf("right opened %q, want the tree to expand only", m.openFolder)
	}
	if !m.folderExpanded["Work"] {
		t.Error("right did not expand Work")
	}
	if text := folderPaneText(m); !strings.Contains(text, "- Work") || !strings.Contains(text, "2024") {
		t.Errorf("pane does not show the markers and children of an expanded folder:\n%s", text)
	}

	m = updateNamed(t, m, "left")

	if m.folderExpanded["Work"] {
		t.Error("left left Work expanded")
	}
	if strings.Contains(folderPaneText(m), "2024") {
		t.Errorf("left did not fold Work:\n%s", folderPaneText(m))
	}

	m = updateNamed(t, m, "right")

	if !strings.Contains(folderPaneText(m), "2024") {
		t.Errorf("right did not expand Work again:\n%s", folderPaneText(m))
	}
}

// TestArrowKeysOnALeafDoNothing is the no-signal case: a folder with nothing under it
// neither expands nor reports anything, because left and right are casual keys.
func TestArrowKeysOnALeafDoNothing(t *testing.T) {
	m := loaded(t, 140, 40, treeFolders(), nil)
	m.folderCursor = 0 // INBOX
	before := folderPaneText(m)

	m = updateNamed(t, m, "left")
	m = updateNamed(t, m, "right")

	if m.status != "" {
		t.Errorf("status = %q, want nothing for a leaf", m.status)
	}
	if m.openFolder != "" {
		t.Errorf("openFolder = %q, want nothing", m.openFolder)
	}
	if got := folderPaneText(m); got != before {
		t.Errorf("pane changed on a leaf:\n%s", got)
	}
}

// TestFolderArrowsAreInertOutsideTheFolderPane proves the guard: left and right do not
// fold the tree while the message list or the reader has focus.
func TestFolderArrowsAreInertOutsideTheFolderPane(t *testing.T) {
	m := loaded(t, 140, 40, treeFolders(), nil)
	m.focus = focusMessages

	m = updateNamed(t, m, "right")
	m = updateNamed(t, m, "left")

	if len(m.folderExpanded) != 0 {
		t.Errorf("expanded = %v, want none while the list has focus", m.folderExpanded)
	}
}

// TestMissingParentIsNotInvented covers the server that lists Work/Projects/2025 but not
// Work/Projects: the folder nests under its nearest listed ancestor and no row is
// conjured for the missing one. A folder with no listed ancestor is a root.
func TestMissingParentIsNotInvented(t *testing.T) {
	m := loaded(t, 140, 40, []mail.Folder{
		{Name: "Work", Delimiter: '/'},
		{Name: "Work/Projects/2025", Delimiter: '/'},
	}, nil)
	m.folderCursor = 0 // Work is the only visible root
	m = updateNamed(t, m, "enter")

	rows := m.folderTree()
	if len(rows) != 2 {
		t.Fatalf("visible rows = %d, want Work and its grandchild", len(rows))
	}
	if got, want := rows[1].depth, 1; got != want {
		t.Errorf("grandchild depth = %d, want %d under the listed grandparent", got, want)
	}

	text := folderPaneText(m)
	if strings.Contains(text, "Projects") {
		t.Errorf("pane invents a Projects row:\n%s", text)
	}
	if !strings.Contains(text, "2025") {
		t.Errorf("pane lost the grandchild:\n%s", text)
	}

	orphan := loaded(t, 140, 40, []mail.Folder{{Name: "A/B/C", Delimiter: '/'}}, nil)
	rows = orphan.folderTree()
	if len(rows) != 1 || rows[0].depth != 0 {
		t.Fatalf("orphan rows = %+v, want one root at depth 0", rows)
	}
	if text := folderPaneText(orphan); !strings.Contains(text, "C") || strings.Contains(text, "B") {
		t.Errorf("orphan pane = %q, want the leaf C at the root", text)
	}
}

// TestFolderToggleIsPersisted proves an explicit fold is written for the active account,
// sorted, and that folding everything removes the account's entry rather than storing an
// empty list.
func TestFolderToggleIsPersisted(t *testing.T) {
	m, rec := stateModel(t, config.State{}, treeFolders())
	m = startModel(t, m)
	m.folderCursor = 1 // Work

	m = updateNamed(t, m, "right")

	if rec.calls != 1 {
		t.Fatalf("state writes = %d, want 1 for the expand", rec.calls)
	}
	if got, want := rec.saved.Expanded["fastmail"], []string{"Work"}; len(got) != 1 || got[0] != want[0] {
		t.Errorf("expanded = %v, want %v", got, want)
	}

	m = updateNamed(t, m, "left")

	if _, ok := rec.saved.Expanded["fastmail"]; ok {
		t.Errorf("expanded still records fastmail after folding everything: %v", rec.saved.Expanded)
	}
}

// TestRememberedExpansionIsApplied proves the state file opens the tree where it was
// left, and that restoring it is not itself a change: no write happens at startup.
func TestRememberedExpansionIsApplied(t *testing.T) {
	sel := config.State{Expanded: map[string][]string{"fastmail": {"Work"}}}
	m, rec := stateModel(t, sel, treeFolders())

	m = startModel(t, m)

	if text := folderPaneText(m); !strings.Contains(text, "2024") {
		t.Errorf("remembered expansion was not applied:\n%s", text)
	}
	if rec.calls != 0 {
		t.Errorf("state writes = %d at startup, want none", rec.calls)
	}
}

// TestRememberedNestedFolderRevealsItsPath proves a remembered folder deep in the tree
// is still visible when the tree defaults to collapsed: its ancestors are opened for
// display, without being recorded as the user's choice.
func TestRememberedNestedFolderRevealsItsPath(t *testing.T) {
	sel := config.State{Folders: map[string]string{"fastmail": "Work/2024/Q1"}}
	m, rec := stateModel(t, sel, treeFolders())
	m.cfg.Accounts[m.account].StartFolder = nil

	m = startModel(t, m)

	if got, want := m.openFolder, "Work/2024/Q1"; got != want {
		t.Errorf("openFolder = %q, want %q", got, want)
	}
	if text := folderPaneText(m); !strings.Contains(text, "Q1") {
		t.Errorf("the open folder's path is hidden:\n%s", text)
	}
	if rec.calls != 0 {
		t.Errorf("state writes = %d, want none: revealing a path is not a user change", rec.calls)
	}
}

// TestFolderCursorIgnoresHiddenRows proves the cursor moves over what is drawn: moving
// down from the last visible row does not land on a folded child.
func TestFolderCursorIgnoresHiddenRows(t *testing.T) {
	m := loaded(t, 140, 40, treeFolders(), nil)
	m.folderCursor = 1 // Work, the last visible row

	m.moveFolderCursor(1)

	if got, want := m.folderCursor, 1; got != want {
		t.Errorf("cursor = %d, want %d: a folded child is not selectable", got, want)
	}
}

// TestFolderTreeSelectionIsBoldNotAGlyph follows the imap-notes shape the owner chose:
// the cursor is the bold row, so it never displaces the +/- marker.
func TestFolderTreeSelectionIsBoldNotAGlyph(t *testing.T) {
	profile := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(profile) })

	m := loaded(t, 140, 40, treeFolders(), nil)
	m.folderCursor = 1 // Work

	rows := m.folderLines(20, 24)
	if strings.Contains(rows[1], "> ") {
		t.Errorf("selected row carries a cursor glyph: %q", rows[1])
	}
	if !boldSGR.MatchString(rows[1]) {
		t.Errorf("selected row is not bold: %q", rows[1])
	}
	if boldSGR.MatchString(rows[0]) {
		t.Errorf("unselected row is bold: %q", rows[0])
	}
}

// TestFolderTreeFrameStaysExact keeps the gap and the tree inside the terminal at the
// supported floor and at the tablet width.
func TestFolderTreeFrameStaysExact(t *testing.T) {
	for _, size := range [][2]int{{60, 20}, {105, 59}, {140, 40}} {
		m := loaded(t, size[0], size[1], treeFolders(), nil)
		m.folderExpanded["Work"] = true
		m.clampFolderScroll()

		assertExactFrame(t, m, "folder tree")
	}
}
