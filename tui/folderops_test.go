package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/renderorange/chute/config"
	"github.com/renderorange/chute/mail"
)

// folderPane loads a listing and puts focus on the folder pane, which is where the
// management keys live.
func folderPane(t *testing.T, folders []mail.Folder) Model {
	t.Helper()

	m := loaded(t, 140, 40, folders, nil)
	m.focus = focusFolders

	return m
}

// folderTree is the listing most tests share: a top-level folder, a nested one, and a
// destination.
func folderTree() []mail.Folder {
	return []mail.Folder{
		{Name: "Archive", Delimiter: '/'},
		{Name: "INBOX", Delimiter: '/'},
		{Name: "Work", Delimiter: '/'},
		{Name: "Work/2026", Delimiter: '/'},
	}
}

// cursorOn puts the tree cursor on a folder, revealing its path.
func cursorOn(t *testing.T, m Model, name string) Model {
	t.Helper()

	folder, ok := m.folderByName(name)
	if !ok {
		t.Fatalf("the listing has no folder %q", name)
	}

	m.moveFolderCursorTo(folder)

	return m
}

// typeName sends every rune of name to the open prompt.
func typeName(t *testing.T, m Model, name string) Model {
	t.Helper()

	for _, r := range name {
		m = press(t, m, keyMsg(string(r)))
	}

	return m
}

// pickDestination puts the picker cursor on a destination and accepts it.
func pickDestination(t *testing.T, m Model, label string) Model {
	t.Helper()

	for i, row := range m.folderMoveMatches {
		if row.label == label {
			m.folderMoveCursor = i

			return press(t, m, tea.KeyMsg{Type: tea.KeyEnter})
		}
	}

	t.Fatalf("the picker has no destination %q, only %v", label, m.folderMoveMatches)

	return m
}

// TestFolderManagementKeysNeedTheFolderPane is the guard that keeps the three new keys
// from firing in the panes that already own a letter: d deletes messages, so D must not
// delete a folder from the message list.
func TestFolderManagementKeysNeedTheFolderPane(t *testing.T) {
	for _, key := range []string{"N", "M", "D"} {
		t.Run(key, func(t *testing.T) {
			m := cursorOn(t, folderPane(t, folderTree()), "Work")
			m.focus = focusMessages

			next := press(t, m, keyMsg(key))

			if next.newFolderOpen || next.folderMoveOpen || next.confirmOpen {
				t.Errorf("%s opened a folder dialog from the message list", key)
			}
			if next.status != "" {
				t.Errorf("%s set status %q outside the folder pane", key, next.status)
			}
		})
	}
}

// TestNewFolderPromptShowsThePathItWillCreate proves the cursor's folder is visible
// before the name is sent, which is what makes "create under the cursor" safe.
func TestNewFolderPromptShowsThePathItWillCreate(t *testing.T) {
	m := cursorOn(t, folderPane(t, folderTree()), "Work")
	m = press(t, m, keyMsg("N"))

	if !m.newFolderOpen {
		t.Fatal("N did not open the create prompt")
	}

	m = typeName(t, m, "2026/Q1")

	if got, want := m.folderNewTarget(), "Work/2026/Q1"; got != want {
		t.Errorf("target = %q, want %q", got, want)
	}
	if view := m.View(); !strings.Contains(view, "will create: Work/2026/Q1") {
		t.Errorf("the prompt does not name the path it will create:\n%s", view)
	}
}

// TestNewFolderCreatesUnderTheCursorFolder covers the whole create flow: the name the
// server is asked for, the listing it returns, and the cursor landing on the new row
// with its parent revealed.
func TestNewFolderCreatesUnderTheCursorFolder(t *testing.T) {
	m := cursorOn(t, folderPane(t, folderTree()), "Work")

	var asked []string
	m.newFolder = func(acct config.Account, name string) ([]mail.Folder, error) {
		asked = append(asked, name)

		folders := folderTree()

		return append(folders, mail.Folder{Name: name, Delimiter: '/'}), nil
	}

	m = press(t, m, keyMsg("N"))
	m = typeName(t, m, "2027")
	m = press(t, m, tea.KeyMsg{Type: tea.KeyEnter})

	if len(asked) != 1 || asked[0] != "Work/2027" {
		t.Fatalf("the server was asked to create %v, want [Work/2027]", asked)
	}
	if !strings.Contains(m.status, "created Work/2027") {
		t.Errorf("status = %q, want the creation named", m.status)
	}
	if _, ok := m.folderByName("Work/2027"); !ok {
		t.Errorf("the listing the server returned was not installed")
	}
	if folder, ok := m.selectedFolder(); !ok || folder.Name != "Work/2027" {
		t.Errorf("the cursor is on %q, want the new folder", folder.Name)
	}
	if !m.folderExpanded["Work"] {
		t.Error("the parent was not revealed, so the new folder would still be hidden")
	}
	if m.newFolderOpen {
		t.Error("the prompt stayed open after the create")
	}
}

// TestNewFolderPromptTakesEveryLetter is the trap the keymap creates: q is quit, and a
// prompt that resolves bindings would make "q" uncancellable to type.
func TestNewFolderPromptTakesEveryLetter(t *testing.T) {
	m := cursorOn(t, folderPane(t, folderTree()), "Work")
	m = press(t, m, keyMsg("N"))
	m = press(t, m, keyMsg("q"))

	if !m.newFolderOpen {
		t.Fatal("q closed the prompt instead of typing into it")
	}
	if got, want := m.folderName, "q"; got != want {
		t.Errorf("name = %q, want %q", got, want)
	}
}

// TestNewFolderRefusesATakenName keeps a duplicate off the server, and says which name is
// in the way.
func TestNewFolderRefusesATakenName(t *testing.T) {
	m := cursorOn(t, folderPane(t, folderTree()), "Work")
	m = press(t, m, keyMsg("N"))
	m = typeName(t, m, "2026")

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)

	if cmd != nil {
		t.Error("a taken name still asked the server to create it")
	}
	if !strings.Contains(m.status, "already exists") {
		t.Errorf("status = %q, want it to say the name is taken", m.status)
	}
}

// TestFolderMovePickerOffersOnlyWorkableDestinations covers the three exclusions: the
// folder itself, its descendants, its current parent, and a destination whose resulting
// name is taken.
func TestFolderMovePickerOffersOnlyWorkableDestinations(t *testing.T) {
	folders := append(folderTree(), mail.Folder{Name: "Archive/Work", Delimiter: '/'})
	m := cursorOn(t, folderPane(t, folders), "Work")

	m = press(t, m, keyMsg("M"))

	if !m.folderMoveOpen {
		t.Fatalf("M did not open the move picker: %q", m.status)
	}

	got := make([]string, 0, len(m.folderMoveMatches))
	for _, row := range m.folderMoveMatches {
		got = append(got, row.label)
	}

	// INBOX is a workable destination, and so is Archive/Work: a mailbox can always hold a
	// child of the same name. Archive is not offered, because the move would produce
	// Archive/Work, which already exists. A top-level row is absent while the folder is
	// already at the top level, and Work/2026 is absent because it is inside the folder being
	// moved.
	want := []string{"INBOX", "Archive/Work"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("destinations = %v, want %v", got, want)
	}
}

// TestFolderMoveConfirmationNamesTheDestination is the issue's "show the destination
// location on confirmation screen", including how many folders travel with it.
func TestFolderMoveConfirmationNamesTheDestination(t *testing.T) {
	m := cursorOn(t, folderPane(t, folderTree()), "Work")
	m = press(t, m, keyMsg("M"))
	m = pickDestination(t, m, "Archive")

	if !m.confirmOpen || m.confirmKind != confirmMoveFolder {
		t.Fatal("choosing a destination did not open the move confirmation")
	}
	if got, want := m.pendingFolderDestination(), "Archive/Work"; got != want {
		t.Errorf("destination = %q, want %q", got, want)
	}

	view := m.View()
	if !strings.Contains(view, "move Work to Archive/Work?") {
		t.Errorf("the confirmation does not name the destination:\n%s", view)
	}
	if !strings.Contains(view, "1 folder move with it") {
		t.Errorf("the confirmation does not say the subtree travels:\n%s", view)
	}
}

// TestFolderMoveRewritesTheOpenFolderAndTheSelection is the bookkeeping a rename needs:
// the reader's folder, the tree's expansion set and the remembered selection all name
// the mailbox, and all three would otherwise point at a name the server no longer has.
func TestFolderMoveRewritesTheOpenFolderAndTheSelection(t *testing.T) {
	m := cursorOn(t, folderPane(t, folderTree()), "Work")
	m.openFolder = "Work/2026"
	m.folderExpanded["Work"] = true
	m.selection.Folders = map[string]string{m.accountName(): "Work/2026"}
	m.selection.Expanded = map[string][]string{m.accountName(): {"Work"}}

	m.moveFolder = func(acct config.Account, name, parent string) (string, []mail.Folder, error) {
		return "Archive/Work", []mail.Folder{
			{Name: "Archive", Delimiter: '/'},
			{Name: "Archive/Work", Delimiter: '/'},
			{Name: "Archive/Work/2026", Delimiter: '/'},
			{Name: "INBOX", Delimiter: '/'},
		}, nil
	}

	m = press(t, m, keyMsg("M"))
	m = pickDestination(t, m, "Archive")
	m = press(t, m, tea.KeyMsg{Type: tea.KeyEnter})

	if got, want := m.openFolder, "Archive/Work/2026"; got != want {
		t.Errorf("openFolder = %q, want %q", got, want)
	}
	if !m.folderExpanded["Archive/Work"] {
		t.Errorf("expansion set = %v, want the renamed ancestor expanded", m.folderExpanded)
	}
	if m.folderExpanded["Work"] {
		t.Error("the expansion set still names the old ancestor")
	}
	if got, want := m.selection.Folders[m.accountName()], "Archive/Work/2026"; got != want {
		t.Errorf("remembered folder = %q, want %q", got, want)
	}
	if got, want := strings.Join(m.selection.Expanded[m.accountName()], ","), "Archive/Work"; got != want {
		t.Errorf("remembered expansion = %q, want %q", got, want)
	}
	if folder, ok := m.selectedFolder(); !ok || folder.Name != "Archive/Work" {
		t.Errorf("the cursor is on %q, want the moved folder", folder.Name)
	}
}

// TestFolderMoveRefusesAMappedFolder protects the setting that resolves a role: a literal
// name does not survive a rename, so the user has to change the config first.
func TestFolderMoveRefusesAMappedFolder(t *testing.T) {
	cfg := testConfig()
	cfg.Accounts[0].IMAP.Trash = "Trash"

	m := loadedWith(t, cfg, []mail.Folder{
		{Name: "INBOX", Delimiter: '/'},
		{Name: "Trash", Delimiter: '/'},
		{Name: "Archive", Delimiter: '/'},
	})
	m.focus = focusFolders
	m = cursorOn(t, m, "Trash")

	m = press(t, m, keyMsg("M"))

	if m.folderMoveOpen {
		t.Fatal("the picker opened for a folder the config names")
	}
	for _, want := range []string{"Trash", "[accounts.imap]", "trash"} {
		if !strings.Contains(m.status, want) {
			t.Errorf("status = %q, want it to name %q", m.status, want)
		}
	}
}

// TestFolderDeleteChecksEmptinessFirst proves the confirmation is only reached for a
// delete that can run, and that the refusal says how much mail is in the way.
func TestFolderDeleteChecksEmptinessFirst(t *testing.T) {
	m := cursorOn(t, folderPane(t, folderTree()), "Work/2026")

	var asked []string
	m.folderMessages = func(acct config.Account, name string) (uint32, error) {
		asked = append(asked, name)

		return 3, nil
	}

	m = press(t, m, keyMsg("D"))

	if len(asked) != 1 || asked[0] != "Work/2026" {
		t.Fatalf("the server was asked about %v, want [Work/2026]", asked)
	}
	if m.confirmOpen {
		t.Error("the confirmation opened for a folder that still holds mail")
	}
	for _, want := range []string{"3 messages", "empty it first"} {
		if !strings.Contains(m.status, want) {
			t.Errorf("status = %q, want it to say %q", m.status, want)
		}
	}
}

// TestFolderDeleteRefusesAFolderWithSubfolders refuses before any round trip: the count
// is in the listing.
func TestFolderDeleteRefusesAFolderWithSubfolders(t *testing.T) {
	m := cursorOn(t, folderPane(t, folderTree()), "Work")

	m.folderMessages = func(config.Account, string) (uint32, error) {
		t.Error("the server was asked about a folder that has subfolders")

		return 0, nil
	}

	m = press(t, m, keyMsg("D"))

	if m.confirmOpen {
		t.Error("the confirmation opened for a folder with subfolders")
	}
	for _, want := range []string{"1 folder", "delete those first"} {
		if !strings.Contains(m.status, want) {
			t.Errorf("status = %q, want it to say %q", m.status, want)
		}
	}
}

// TestFolderDeleteConfirmsAnEmptyFolder covers the confirmation and the call that removes
// the mailbox.
func TestFolderDeleteConfirmsAnEmptyFolder(t *testing.T) {
	m := cursorOn(t, folderPane(t, folderTree()), "Work/2026")
	m.folderMessages = func(config.Account, string) (uint32, error) { return 0, nil }

	var removed []string
	m.deleteFolder = func(acct config.Account, name string) ([]mail.Folder, error) {
		removed = append(removed, name)

		return []mail.Folder{
			{Name: "Archive", Delimiter: '/'},
			{Name: "INBOX", Delimiter: '/'},
			{Name: "Work", Delimiter: '/'},
		}, nil
	}

	m = press(t, m, keyMsg("D"))

	if !m.confirmOpen || m.confirmKind != confirmDeleteFolder {
		t.Fatal("an empty folder did not reach the delete confirmation")
	}
	if view := m.View(); !strings.Contains(view, "delete Work/2026?") {
		t.Errorf("the confirmation does not name the folder:\n%s", view)
	}

	m = press(t, m, tea.KeyMsg{Type: tea.KeyEnter})

	if len(removed) != 1 || removed[0] != "Work/2026" {
		t.Fatalf("the server was asked to delete %v, want [Work/2026]", removed)
	}
	if _, ok := m.folderByName("Work/2026"); ok {
		t.Error("the deleted folder is still in the listing")
	}
	if !strings.Contains(m.status, "deleted Work/2026") {
		t.Errorf("status = %q, want the delete named", m.status)
	}
}

// TestDeletingTheOpenFolderMovesTheReaderOn keeps the reader off a mailbox that no longer
// exists: the folder that followed it in the listing opens instead.
func TestDeletingTheOpenFolderMovesTheReaderOn(t *testing.T) {
	folders := []mail.Folder{
		{Name: "Archive", Delimiter: '/'},
		{Name: "INBOX", Delimiter: '/'},
		{Name: "Work", Delimiter: '/'},
	}

	m := cursorOn(t, folderPane(t, folders), "Work")
	m.openFolder = "Work"
	m.messages = sampleMessages(2)
	m.loadMessages = fakeMessages(nil, nil)
	m.folderMessages = func(config.Account, string) (uint32, error) { return 0, nil }
	m.deleteFolder = func(config.Account, string) ([]mail.Folder, error) {
		return []mail.Folder{
			{Name: "Archive", Delimiter: '/'},
			{Name: "INBOX", Delimiter: '/'},
		}, nil
	}

	m = press(t, m, keyMsg("D"))
	m = press(t, m, tea.KeyMsg{Type: tea.KeyEnter})

	if got, want := m.openFolder, "INBOX"; got != want {
		t.Errorf("openFolder = %q, want the folder that followed %q", got, want)
	}
	for _, want := range []string{"deleted Work", "opened INBOX"} {
		if !strings.Contains(m.status, want) {
			t.Errorf("status = %q, want it to say %q", m.status, want)
		}
	}
}

// TestFolderOverlayRunsTheFolderKeys keeps folder management reachable in the two-column
// layout, which draws no folder pane.
func TestFolderOverlayRunsTheFolderKeys(t *testing.T) {
	m := folderPane(t, folderTree())
	m.foldersOpen = true
	for i, folder := range m.folders {
		if folder.Name == "Work" {
			m.folderOverlayCursor = i
		}
	}

	m = press(t, m, keyMsg("M"))

	if !m.folderMoveOpen {
		t.Fatalf("the overlay did not open the move picker: %q", m.status)
	}
	if got, want := m.folderMoveName, "Work"; got != want {
		t.Errorf("moving %q, want %q", got, want)
	}
}

// TestFolderDialogsFitTheMinimumFrame covers the 60x20 floor the layout is tested at:
// a folder name long enough to fill the frame must not widen a dialog.
func TestFolderDialogsFitTheMinimumFrame(t *testing.T) {
	long := "A very long folder name that runs past the frame at sixty columns wide"
	folders := []mail.Folder{
		{Name: "Archive", Delimiter: '/'},
		{Name: "INBOX", Delimiter: '/'},
		{Name: long, Delimiter: '/'},
	}

	base := loaded(t, 60, 20, folders, nil)
	base.focus = focusFolders
	base.moveFolderCursorTo(folders[2])

	create := press(t, base, keyMsg("N"))
	create = typeName(t, create, long)
	assertExactFrame(t, create, "create prompt at 60x20")

	mover := press(t, base, keyMsg("M"))
	if !mover.folderMoveOpen {
		t.Fatalf("the picker did not open: %q", mover.status)
	}
	assertExactFrame(t, mover, "move picker at 60x20")

	confirm := pickDestination(t, mover, "Archive")
	if !confirm.confirmOpen {
		t.Fatal("choosing a destination did not open the confirmation")
	}
	assertExactFrame(t, confirm, "move confirmation at 60x20")

	remove := cursorOn(t, base, "Archive")
	remove.folderMessages = func(config.Account, string) (uint32, error) { return 0, nil }
	remove = press(t, remove, keyMsg("D"))
	assertExactFrame(t, remove, "delete confirmation at 60x20")
}

// TestFolderOpFailureKeepsTheListing proves a refused operation leaves the tree as it was
// and reports the reason, rather than half-applying a listing the server never sent.
func TestFolderOpFailureKeepsTheListing(t *testing.T) {
	m := cursorOn(t, folderPane(t, folderTree()), "Work")
	m.newFolder = func(config.Account, string) ([]mail.Folder, error) {
		return nil, &mail.FolderNotEmptyError{Folder: "Work", Count: 4}
	}

	m = press(t, m, keyMsg("N"))
	m = typeName(t, m, "2027")
	m = press(t, m, tea.KeyMsg{Type: tea.KeyEnter})

	if len(m.folders) != len(folderTree()) {
		t.Errorf("folders = %d, want the listing left alone", len(m.folders))
	}
	for _, want := range []string{"4 messages", "empty it first"} {
		if !strings.Contains(m.status, want) {
			t.Errorf("status = %q, want it to say %q", m.status, want)
		}
	}
}
