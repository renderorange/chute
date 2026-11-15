package tui

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/emersion/go-imap/v2"
	"github.com/renderorange/chute/mail"
	"github.com/sahilm/fuzzy"
)

// Folder management: the pieces the create prompt, the move picker and the delete check
// share. The rules about which names are legal and which folders may be touched live in
// the mail package, so the sentence the interface shows before an action is the one the
// session would return after it.

// folderParent is one destination in the move picker: a mailbox that can hold the moved
// folder, or the top level when name is empty.
type folderParent struct {
	name  string
	label string
}

// folderActionsReady reports whether a folder-management key should act. The folder pane
// owns these keys; the overlay is the same list in the two-column layout, which has no
// folder pane, so it owns them there too.
func (m Model) folderActionsReady() bool {
	return m.focus == focusFolders || m.foldersOpen
}

// folderActionTarget is the folder a management action applies to: the row under the
// tree cursor, or the row under the overlay cursor while the overlay is open.
func (m Model) folderActionTarget() (mail.Folder, bool) {
	if m.foldersOpen {
		if m.folderOverlayCursor < 0 || m.folderOverlayCursor >= len(m.folders) {
			return mail.Folder{}, false
		}

		return m.folders[m.folderOverlayCursor], true
	}

	return m.selectedFolder()
}

// folderDelimiter is the hierarchy separator the server advertised. Zero means the
// server named none, which makes every mailbox a top-level name.
func (m Model) folderDelimiter() rune {
	for _, folder := range m.folders {
		if folder.Delimiter != 0 {
			return folder.Delimiter
		}
	}

	return 0
}

// folderDescendants counts the folders below name, at any depth. Delete refuses while
// this is non-zero, so a mistaken press cannot orphan or destroy a subtree.
func (m Model) folderDescendants(name string) int {
	delimiter := m.folderDelimiter()

	count := 0
	for _, folder := range m.folders {
		if mail.IsBelow(folder.Name, name, delimiter) {
			count++
		}
	}

	return count
}

// folderPath joins a parent and a leaf with the server's delimiter. An empty parent
// means the top level.
func (m Model) folderPath(parent, leaf string) string {
	delimiter := m.folderDelimiter()
	if parent == "" || delimiter == 0 {
		return leaf
	}

	return parent + string(delimiter) + leaf
}

// folderNewTarget is the full name the create prompt would send, or "" while the field
// is empty.
func (m Model) folderNewTarget() string {
	typed := strings.TrimSpace(m.folderName)
	if typed == "" {
		return ""
	}

	return m.folderPath(m.folderBase.Name, typed)
}

// folderNewProblem explains why the typed name cannot be created, or returns "". The
// check is the session's own, so the prompt refuses exactly what the server would.
func (m Model) folderNewProblem() string {
	typed := strings.TrimSpace(m.folderName)
	if typed == "" {
		return ""
	}

	if _, err := mail.FolderPath(typed, m.folderDelimiter()); err != nil {
		return err.Error()
	}

	if _, exists := m.folderByName(m.folderNewTarget()); exists {
		return "a folder named " + m.folderNewTarget() + " already exists"
	}

	return ""
}

// folderParentRows lists the destinations a move can use: the top level and every
// mailbox that is not the folder itself, not one of its descendants, not its current
// parent, and not a name the move would collide with. A destination that cannot work is
// noise in a list the user is choosing from, the same rule the message-move picker uses.
func (m Model) folderParentRows(folder mail.Folder) []folderParent {
	delimiter := m.folderDelimiter()

	rows := make([]folderParent, 0, len(m.folders))
	if _, exists := m.folderByName(folder.Leaf()); !exists {
		rows = append(rows, folderParent{name: "", label: "(top level)"})
	}

	for _, candidate := range m.folders {
		if sameFolder(candidate.Name, folder.Name) {
			continue
		}
		if mail.IsBelow(candidate.Name, folder.Name, delimiter) {
			continue
		}
		if sameFolder(candidate.Name, folder.Parent()) {
			continue
		}
		if _, exists := m.folderByName(m.folderPath(candidate.Name, folder.Leaf())); exists {
			continue
		}

		rows = append(rows, folderParent{name: candidate.Name, label: candidate.Name})
	}

	return rows
}

// matchFolderParents narrows the destination list, using the same fuzzy, case-insensitive
// rules as the message-move picker and the list filter.
func matchFolderParents(rows []folderParent, query string) []folderParent {
	query = strings.TrimSpace(query)
	if query == "" {
		return rows
	}

	labels := make([]string, len(rows))
	for i, row := range rows {
		labels[i] = row.label
	}

	matched := make([]folderParent, 0, len(rows))
	for _, hit := range fuzzy.FindNoSort(query, labels) {
		matched = append(matched, rows[hit.Index])
	}

	return matched
}

// applyFolderList installs the listing a folder operation produced and reveals the
// folder it names. The listing is the server's answer, never a locally edited tree, so a
// server that normalises a name shows its version.
func (m *Model) applyFolderList(folders []mail.Folder, selectName string) {
	m.folders = folders
	m.state = stateConnected

	if selectName != "" {
		m.expandTo(selectName)

		if index := m.treeRowIndex(selectName); index >= 0 {
			m.folderCursor = index
		}
		if index := m.folderIndexByName(selectName); index >= 0 {
			m.folderOverlayCursor = index
		}
	}

	// A delete names no folder to select, and the row the overlay cursor pointed at may
	// have been the one removed.
	m.folderOverlayCursor = clamp(m.folderOverlayCursor, 0, max(0, len(folders)-1))
	m.clampFolderScroll()
}

// folderIndexByName is the position of a mailbox in the flat listing, which is what the
// overlay cursor indexes. It is -1 when the listing does not have the name.
func (m Model) folderIndexByName(name string) int {
	for i, folder := range m.folders {
		if folder.Name == name {
			return i
		}
	}

	return -1
}

// renameFolderState follows a move through everything that names a mailbox: the open
// folder, the tree's expansion set, the cached bodies and the selection the state file
// remembers. Without it the next load would select a name the server no longer has.
func (m *Model) renameFolderState(oldName, newName string) {
	delimiter := m.folderDelimiter()

	if moved := renamedPath(m.openFolder, oldName, newName, delimiter); moved != m.openFolder {
		m.forgetFolderSubtree(m.openFolder)
		m.openFolder = moved
	}

	if len(m.folderExpanded) > 0 {
		expanded := make(map[string]bool, len(m.folderExpanded))
		for name, open := range m.folderExpanded {
			expanded[renamedPath(name, oldName, newName, delimiter)] = open
		}

		m.folderExpanded = expanded
		m.rememberExpanded()
	}

	if name, ok := m.selection.Folders[m.accountName()]; ok {
		m.selection.Folders[m.accountName()] = renamedPath(name, oldName, newName, delimiter)
	}
	if names, ok := m.selection.Expanded[m.accountName()]; ok {
		rewritten := make([]string, 0, len(names))
		for _, name := range names {
			rewritten = append(rewritten, renamedPath(name, oldName, newName, delimiter))
		}
		sort.Strings(rewritten)

		m.selection.Expanded[m.accountName()] = rewritten
	}

	m.rememberSelection()
}

// forgetFolderSubtree drops the cached state of messages in a mailbox that moved or
// disappeared. The bodies are keyed by the old path, and a stale key would either miss
// the renamed folder or answer for a mailbox that is gone.
func (m *Model) forgetFolderSubtree(name string) {
	if name == "" {
		return
	}

	in := func(folder string) bool {
		return sameFolder(folder, name) || mail.IsBelow(folder, name, m.folderDelimiter())
	}

	for key := range m.bodies {
		if in(key.folder) {
			delete(m.bodies, key)
		}
	}
	for key := range m.markSeenOnLoad {
		if in(key.folder) {
			delete(m.markSeenOnLoad, key)
		}
	}
	for key := range m.lastSeen {
		if in(key.folder) {
			delete(m.lastSeen, key)
		}
	}

	if in(m.loadingBody.folder) {
		m.loadingBody = bodyKey{}
	}
}

// folderAfterDelete is where the reader lands when name is deleted: the next selectable
// mailbox after it in the listing, else the previous one, else none.
func (m Model) folderAfterDelete(name string) (string, bool) {
	index := -1
	for i, folder := range m.folders {
		if sameFolder(folder.Name, name) {
			index = i

			break
		}
	}
	if index < 0 {
		return "", false
	}

	for i := index + 1; i < len(m.folders); i++ {
		if m.folders[i].Selectable() {
			return m.folders[i].Name, true
		}
	}
	for i := index - 1; i >= 0; i-- {
		if m.folders[i].Selectable() {
			return m.folders[i].Name, true
		}
	}

	return "", false
}

// clearFolderFromState forgets a deleted mailbox in the tree's expansion set and in the
// selection the state file remembers, so neither names a folder the server has dropped.
func (m *Model) clearFolderFromState(name string) {
	delimiter := m.folderDelimiter()

	for key := range m.folderExpanded {
		if sameFolder(key, name) || mail.IsBelow(key, name, delimiter) {
			delete(m.folderExpanded, key)
		}
	}

	if sameFolder(m.selection.Folders[m.accountName()], name) {
		delete(m.selection.Folders, m.accountName())
	}
	if names, ok := m.selection.Expanded[m.accountName()]; ok {
		kept := make([]string, 0, len(names))
		for _, expanded := range names {
			if sameFolder(expanded, name) || mail.IsBelow(expanded, name, delimiter) {
				continue
			}

			kept = append(kept, expanded)
		}

		if len(kept) == 0 {
			delete(m.selection.Expanded, m.accountName())
		} else {
			m.selection.Expanded[m.accountName()] = kept
		}
	}

	m.rememberExpanded()
}

// pendingFolderDestination is the full name the folder a confirmation is about will have
// after the move.
func (m Model) pendingFolderDestination() string {
	folder := mail.Folder{Name: m.pendingFolder, Delimiter: m.folderDelimiter()}

	return m.folderPath(m.pendingFolderParent, folder.Leaf())
}

// pendingFolderRole is the role the folder a confirmation is about serves, or RoleNone.
// A delete confirmation says so, because losing the sent or drafts mailbox is not
// obvious from the folder's name.
func (m Model) pendingFolderRole() mail.Role {
	folder, ok := m.folderByName(m.pendingFolder)
	if !ok {
		return mail.RoleNone
	}

	return folder.Role()
}

// applyFolderCreated installs the listing after a create and names it in the status bar.
func (m *Model) applyFolderCreated(msg folderCreatedMsg) {
	if msg.err != nil {
		m.status = folderOpStatus(msg.err)

		return
	}

	m.applyFolderList(msg.folders, msg.name)
	m.status = "created " + msg.name
}

// applyFolderMoved installs the listing after a move and follows the folder through the
// state that names it.
func (m *Model) applyFolderMoved(msg folderMovedMsg) {
	if msg.err != nil {
		m.status = folderOpStatus(msg.err)

		return
	}

	m.renameFolderState(msg.oldName, msg.newName)
	m.applyFolderList(msg.folders, msg.newName)
	m.status = "moved " + msg.oldName + " to " + msg.newName
}

// applyFolderDeleted installs the listing after a delete, and moves the reader on when
// the folder it was reading is the one that went.
func (m Model) applyFolderDeleted(msg folderDeletedMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.status = folderOpStatus(msg.err)

		return m, nil
	}

	// Where to land is a fact about the old listing, so it is read before it changes.
	following, hasFollowing := m.folderAfterDelete(msg.name)
	wasOpen := sameFolder(m.openFolder, msg.name)

	m.forgetFolderSubtree(msg.name)
	m.clearFolderFromState(msg.name)
	m.applyFolderList(msg.folders, "")

	if !wasOpen {
		m.status = "deleted " + msg.name

		return m, nil
	}

	m.openFolder = ""
	m.messages = nil
	m.rows = nil
	m.filter = ""
	m.messageCursor = 0
	m.messageScroll = 0
	m.readerScroll = 0
	m.visualAnchor = noVisualAnchor
	m.collapsed = make(map[imap.UID]bool)
	m.loadingBody = bodyKey{}
	m.openUnseen = 0
	m.clearSearch()

	if !hasFollowing {
		m.focus = focusFolders
		m.status = "deleted " + msg.name

		return m, nil
	}

	next, cmd := m.openFolderByName(following, true)
	// openFolderByName clears the status, so the confirmation has to follow it.
	nextTyped := next.(Model)
	nextTyped.status = "deleted " + msg.name + "; opened " + following

	return nextTyped, cmd
}

// applyFolderChecked opens the delete confirmation when the server says the folder is
// empty, and explains what is in the way when it does not.
func (m Model) applyFolderChecked(msg folderCheckedMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.status = folderOpStatus(msg.err)

		return m, nil
	}

	if msg.count > 0 {
		m.status = fmt.Sprintf("%s has %s; empty it first", msg.name, countLabel(int(msg.count), "message"))

		return m, nil
	}

	folder, ok := m.folderByName(msg.name)
	if !ok {
		m.status = "no folder selected"

		return m, nil
	}

	m.confirmOpen = true
	m.confirmKind = confirmDeleteFolder
	m.pendingFolder = folder.Name
	m.pendingFolderParent = ""
	m.pendingFolderBelow = 0
	m.status = ""

	return m, nil
}

// deleteFolderCmd removes the confirmed folder in the background.
func (m Model) deleteFolderCmd(name string) tea.Cmd {
	if m.deleteFolder == nil || len(m.cfg.Accounts) == 0 {
		return nil
	}

	acct := m.cfg.Accounts[m.account]
	remove := m.deleteFolder

	return func() tea.Msg {
		folders, err := remove(acct, name)

		return folderDeletedMsg{account: acct.Name, name: name, folders: folders, err: err}
	}
}

// moveFolderCmd renames the confirmed folder in the background.
func (m Model) moveFolderCmd(name, parent string) tea.Cmd {
	if m.moveFolder == nil || len(m.cfg.Accounts) == 0 {
		return nil
	}

	acct := m.cfg.Accounts[m.account]
	move := m.moveFolder

	return func() tea.Msg {
		newName, folders, err := move(acct, name, parent)

		return folderMovedMsg{account: acct.Name, oldName: name, newName: newName, folders: folders, err: err}
	}
}

// folderOpStatus renders a folder-operation failure. The counted refusals name the
// number that blocks the action: "not empty" without a count does not tell the user what
// to do next.
func folderOpStatus(err error) string {
	var notEmpty *mail.FolderNotEmptyError
	if errors.As(err, &notEmpty) {
		return fmt.Sprintf("%s has %s; empty it first", notEmpty.Folder, countLabel(int(notEmpty.Count), "message"))
	}

	var children *mail.FolderHasChildrenError
	if errors.As(err, &children) {
		return fmt.Sprintf("%s has %s below it; delete those first", children.Folder, countLabel(children.Count, "folder"))
	}

	switch {
	case errors.Is(err, mail.ErrFolderExists):
		return "that folder already exists"

	case errors.Is(err, mail.ErrFolderProtected):
		return "that folder cannot be changed"

	case errors.Is(err, mail.ErrFolderCycle):
		return "a folder cannot move inside itself"

	case errors.Is(err, mail.ErrFolderNoOp):
		return "already there"
	}

	return statusText(err)
}

// sameFolder compares mailbox names the way the server does.
func sameFolder(a, b string) bool {
	return strings.EqualFold(a, b)
}

// renamedPath rewrites name when oldName becomes newName: the folder itself, or anything
// below it. A name outside the subtree is returned unchanged.
func renamedPath(name, oldName, newName string, delimiter rune) string {
	if name == "" {
		return ""
	}
	if sameFolder(name, oldName) {
		return newName
	}
	if !mail.IsBelow(name, oldName, delimiter) {
		return name
	}

	return newName + string(delimiter) + name[len(oldName)+len(string(delimiter)):]
}
