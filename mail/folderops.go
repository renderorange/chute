package mail

import (
	"fmt"
	"strings"

	"github.com/renderorange/chute/config"
)

// Folder management: create, move and delete a mailbox.
//
// Servers disagree about how much of the work a single command does. RFC 3501 tells a
// server to create missing parents for CREATE and to rename inferior names for RENAME,
// but the in-process server this package tests against does neither, and neither do
// some real ones. So this file does that work itself: it creates every missing
// ancestor, and after a rename it repairs any descendant the server left behind.
//
// Every operation ends by listing the mailboxes again and returning what the server
// reported. The interface draws that listing rather than a locally edited tree, so a
// server that normalises a name shows its version.

// roleConfigKeys names the [accounts.imap] settings that pin a role to a mailbox, in
// the order a refusal lists them. The values are the config keys, not the roles, so the
// message can tell the user which line to change.
var roleConfigKeys = []struct {
	role Role
	key  string
}{
	{RoleSent, "sent"},
	{RoleDrafts, "drafts"},
	{RoleTrash, "trash"},
	{RoleArchive, "archive"},
	{RoleJunk, "junk"},
}

// NewFolder creates the mailbox named name, making any missing ancestors on the way.
// name is a full path whose levels are separated by the account's delimiter.
func (s *Session) NewFolder(name string) ([]Folder, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.ensureListingLocked(); err != nil {
		return nil, err
	}

	segments, err := FolderPath(name, s.delimiterLocked())
	if err != nil {
		return nil, fmt.Errorf("mail: account %q: %w", s.acct.Name, err)
	}

	full := strings.Join(segments, string(s.delimiterLocked()))
	if _, exists := s.folderNamedLocked(full); exists {
		return nil, fmt.Errorf("%w: account %q: %q", ErrFolderExists, s.acct.Name, full)
	}

	client, err := s.acquireLocked()
	if err != nil {
		return nil, err
	}

	// One CREATE per level, shallowest first. A level the listing already has is
	// skipped: creating it again is what the server would refuse.
	for i := range segments {
		path := strings.Join(segments[:i+1], string(s.delimiterLocked()))
		if _, exists := s.folderNamedLocked(path); exists {
			continue
		}
		if err := client.CreateMailbox(path); err != nil {
			s.resetLocked()
			return nil, err
		}
	}

	return s.refreshFoldersLocked(client)
}

// MoveFolder renames name so that it sits under parent, keeping its leaf. An empty
// parent moves it to the top level. It returns the folder's new full name and the
// listing the server confirmed.
func (s *Session) MoveFolder(name, parent string) (string, []Folder, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.ensureListingLocked(); err != nil {
		return "", nil, err
	}

	folder, ok := s.folderNamedLocked(name)
	if !ok {
		return "", nil, fmt.Errorf("mail: account %q: no mailbox named %q", s.acct.Name, name)
	}
	if err := s.refuseProtectedLocked(folder); err != nil {
		return "", nil, err
	}

	delimiter := s.delimiterLocked()
	parent = strings.TrimSpace(parent)
	dest := folder.Leaf()
	if parent != "" {
		if _, ok := s.folderNamedLocked(parent); !ok {
			return "", nil, fmt.Errorf("mail: account %q: no mailbox named %q", s.acct.Name, parent)
		}
		dest = parent + string(delimiter) + folder.Leaf()
	}

	switch {
	case strings.EqualFold(dest, folder.Name):
		return dest, nil, fmt.Errorf("%w: account %q: %q", ErrFolderNoOp, s.acct.Name, folder.Name)

	case insideFolder(parent, folder.Name, delimiter):
		return dest, nil, fmt.Errorf("%w: account %q: %q is inside %q", ErrFolderCycle, s.acct.Name, parent, folder.Name)
	}

	if _, exists := s.folderNamedLocked(dest); exists {
		return dest, nil, fmt.Errorf("%w: account %q: %q", ErrFolderExists, s.acct.Name, dest)
	}

	client, err := s.acquireLocked()
	if err != nil {
		return dest, nil, err
	}

	if err := client.RenameMailbox(folder.Name, dest); err != nil {
		s.resetLocked()
		return dest, nil, err
	}

	folders, repaired, err := s.moveDescendantsLocked(client, folder.Name, dest, delimiter)
	if err != nil {
		s.resetLocked()
		return dest, nil, err
	}
	if repaired {
		if folders, err = s.listLocked(client); err != nil {
			s.resetLocked()
			return dest, nil, err
		}
	}

	s.folders = folders

	return dest, FilterFolders(folders, s.acct.IMAP.HideFolders), nil
}

// DeleteFolder deletes an empty mailbox. It refuses a folder that still holds mail or
// has folders below it, because neither outcome is what the user asked for: the first
// loses mail, the second orphans or destroys a subtree depending on the server.
func (s *Session) DeleteFolder(name string) ([]Folder, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.ensureListingLocked(); err != nil {
		return nil, err
	}

	folder, ok := s.folderNamedLocked(name)
	if !ok {
		return nil, fmt.Errorf("mail: account %q: no mailbox named %q", s.acct.Name, name)
	}
	if err := s.refuseProtectedLocked(folder); err != nil {
		return nil, err
	}
	if below := s.descendantCountLocked(folder.Name); below > 0 {
		return nil, &FolderHasChildrenError{Folder: folder.Name, Count: below}
	}

	client, err := s.acquireLocked()
	if err != nil {
		return nil, err
	}

	// The count comes from the server, not from the cached listing: the listing holds
	// folder names, not folder contents, and another client can deliver mail between
	// the interface's check and this one. A container marked \Noselect holds no mail by
	// definition, and some servers refuse STATUS on one.
	var count uint32
	if folder.Selectable() {
		count, err = client.MailboxMessages(folder.Name)
		if err != nil {
			s.resetLocked()
			return nil, err
		}
	}
	if count > 0 {
		return nil, &FolderNotEmptyError{Folder: folder.Name, Count: count}
	}

	if err := client.DeleteMailbox(folder.Name); err != nil {
		s.resetLocked()
		return nil, err
	}

	return s.refreshFoldersLocked(client)
}

// FolderConflict reports the sentence explaining why a rename or delete of folder is
// refused, or "" when it is allowed. The interface asks before it offers the action and
// the session asks again before it runs it, so both renderings come from this one rule.
//
// INBOX is the protocol's special case: renaming it moves its messages out and leaves an
// empty INBOX behind, and deleting it is not allowed at all. A folder a config override
// names is blocked because a literal name does not travel through a rename; a folder the
// server marks with an attribute does, so those stay editable.
func FolderConflict(acct config.Account, folder Folder) string {
	if folder.Role() == RoleInbox {
		return "INBOX cannot be moved or deleted"
	}

	for _, setting := range roleConfigKeys {
		override := strings.TrimSpace(roleOverride(acct, setting.role))
		if override != "" && strings.EqualFold(override, folder.Name) {
			return fmt.Sprintf("%q is set as %s in [accounts.imap]; change that setting first", folder.Name, setting.key)
		}
	}

	return ""
}

// refuseProtectedLocked types the conflict for the session's callers.
func (s *Session) refuseProtectedLocked(folder Folder) error {
	reason := FolderConflict(s.acct, folder)
	if reason == "" {
		return nil
	}

	return fmt.Errorf("%w: account %q: %s", ErrFolderProtected, s.acct.Name, reason)
}

// moveDescendantsLocked renames every folder the server left behind under the old name.
// A server that cascades the rename itself leaves nothing to find, which makes this a
// LIST and no writes. The repairs run shallowest first, so each new parent exists
// before its children are renamed into it.
func (s *Session) moveDescendantsLocked(client sessionClient, oldName, newName string, delimiter rune) ([]Folder, bool, error) {
	if delimiter == 0 {
		return nil, false, nil
	}

	folders, err := s.listLocked(client)
	if err != nil {
		return nil, false, err
	}

	var left []Folder
	for _, folder := range folders {
		if IsBelow(folder.Name, oldName, delimiter) {
			left = append(left, folder)
		}
	}

	// Deepest last: the parent must exist at its new path before a child is moved
	// under it.
	for i := 1; i < len(left); i++ {
		for j := i; j > 0 && left[j].Depth() < left[j-1].Depth(); j-- {
			left[j], left[j-1] = left[j-1], left[j]
		}
	}

	for _, folder := range left {
		// Drop the ancestor's own levels from the child's path and graft the new ones
		// on, rather than slicing bytes: a server that returned the name in a different
		// case still yields the right suffix.
		levels := strings.Split(folder.Name, string(delimiter))
		drop := strings.Count(oldName, string(delimiter)) + 1
		if len(levels) <= drop {
			continue
		}

		dest := newName + string(delimiter) + strings.Join(levels[drop:], string(delimiter))
		if err := client.RenameMailbox(folder.Name, dest); err != nil {
			return nil, true, err
		}
	}

	return folders, len(left) > 0, nil
}

// ensureListingLocked loads the mailbox list when there is none. Every refusal in this
// file reads that list, and the interface has always listed before it offers an action,
// so this only covers a programmatic first call.
func (s *Session) ensureListingLocked() error {
	if len(s.folders) > 0 {
		return nil
	}

	client, err := s.acquireLocked()
	if err != nil {
		return err
	}
	if _, err := s.listLocked(client); err != nil {
		s.resetLocked()
		return err
	}

	return nil
}

// listLocked lists every mailbox and caches the unfiltered result, which is what role
// resolution reads even when hide_folders keeps a mailbox off screen.
func (s *Session) listLocked(client sessionClient) ([]Folder, error) {
	folders, err := client.Folders()
	if err != nil {
		return nil, err
	}

	s.folders = folders

	return folders, nil
}

// FolderMessages counts the messages in a mailbox with one STATUS, without selecting
// it. The interface asks before it offers to delete a folder, so the confirmation it
// shows is never about an action the server would refuse.
func (s *Session) FolderMessages(name string) (uint32, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.ensureListingLocked(); err != nil {
		return 0, err
	}

	folder, ok := s.folderNamedLocked(name)
	if !ok {
		return 0, fmt.Errorf("mail: account %q: no mailbox named %q", s.acct.Name, name)
	}
	if !folder.Selectable() {
		// A \Noselect container holds no mail by definition, and some servers refuse
		// STATUS on one.
		return 0, nil
	}

	client, err := s.acquireLocked()
	if err != nil {
		return 0, err
	}

	count, err := client.MailboxMessages(folder.Name)
	if err != nil {
		s.resetLocked()
		return 0, err
	}

	return count, nil
}

// refreshFoldersLocked lists the mailboxes after a change and returns the view the
// interface draws.
func (s *Session) refreshFoldersLocked(client sessionClient) ([]Folder, error) {
	folders, err := s.listLocked(client)
	if err != nil {
		s.resetLocked()
		return nil, err
	}

	return FilterFolders(folders, s.acct.IMAP.HideFolders), nil
}

// delimiterLocked is the hierarchy separator the server advertises. Zero means the
// server named none, which makes every mailbox a top-level name.
func (s *Session) delimiterLocked() rune {
	for _, folder := range s.folders {
		if folder.Delimiter != 0 {
			return folder.Delimiter
		}
	}

	return 0
}

// descendantCountLocked counts the folders below name, at any depth.
func (s *Session) descendantCountLocked(name string) int {
	delimiter := s.delimiterLocked()

	count := 0
	for _, folder := range s.folders {
		if IsBelow(folder.Name, name, delimiter) {
			count++
		}
	}

	return count
}

// FolderPath splits a typed path into levels and rejects the shapes that cannot
// name a mailbox. A server with no delimiter gets one level, which is what its names
// are.
//
// The messages are user-facing: the create prompt shows them as the user types, so the
// refusal it previews is the one the session would return.
func FolderPath(name string, delimiter rune) ([]string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, fmt.Errorf("a folder needs a name")
	}

	for _, r := range name {
		if r < 0x20 || r == 0x7f {
			return nil, fmt.Errorf("%q holds a control character", name)
		}
	}

	if delimiter == 0 {
		return []string{name}, nil
	}

	sep := string(delimiter)
	if strings.HasPrefix(name, sep) || strings.HasSuffix(name, sep) {
		return nil, fmt.Errorf("%q cannot start or end with %q", name, delimiter)
	}

	segments := strings.Split(name, sep)
	for _, segment := range segments {
		if strings.TrimSpace(segment) == "" {
			return nil, fmt.Errorf("%q has an empty level", name)
		}
	}

	return segments, nil
}

// IsBelow reports whether name sits under ancestor in the hierarchy. The comparison is
// case-insensitive because mailbox names are matched that way everywhere else.
func IsBelow(name, ancestor string, delimiter rune) bool {
	if delimiter == 0 || len(name) <= len(ancestor)+1 {
		return false
	}
	if !strings.EqualFold(name[:len(ancestor)], ancestor) {
		return false
	}

	return rune(name[len(ancestor)]) == delimiter
}

// insideFolder reports whether name is candidate itself or one of its descendants.
func insideFolder(name, candidate string, delimiter rune) bool {
	if name == "" {
		return false
	}

	return strings.EqualFold(name, candidate) || IsBelow(name, candidate, delimiter)
}
