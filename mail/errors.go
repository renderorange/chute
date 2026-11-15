package mail

import (
	"errors"
	"fmt"
)

// Sentinel errors the interface maps onto user-facing phrases. The wrapped text keeps
// the account, host, stage and original cause for logs; the TUI decides the wording.
// This is the precedent ErrAlreadyInTrash set in #14.
var (
	// ErrConnect reports that the IMAP or SMTP connection could not be opened.
	ErrConnect = errors.New("mail: cannot connect")

	// ErrAuth reports that the server rejected the login.
	ErrAuth = errors.New("mail: authentication rejected")

	// ErrNoMoveSupport reports a server that advertises neither MOVE nor UIDPLUS, so
	// delete, archive and junk cannot move mail without a folder-wide EXPUNGE.
	ErrNoMoveSupport = errors.New("mail: server cannot move mail")

	// ErrAlreadyInTrash reports that a delete was asked for inside the trash mailbox
	// itself. It is typed so the interface can say "already in Trash" instead of
	// echoing the wrapped account context at the user.
	ErrAlreadyInTrash = errors.New("mail: already the trash folder")

	// ErrAlreadyInRoleFolder reports a move into the mailbox the messages already live
	// in. It is the archive and junk analogue of ErrAlreadyInTrash.
	ErrAlreadyInRoleFolder = errors.New("mail: already in that folder")

	// ErrNoRoleFolder reports that no mailbox serves a role and no override names one.
	// Callers match it with errors.Is to distinguish "cannot file this copy" from a real
	// protocol failure.
	ErrNoRoleFolder = errors.New("mail: no mailbox serves the role")

	// ErrNoSafePurge reports a server that cannot expunge named UIDs (no UIDPLUS), so a
	// permanent delete of a selection would need a folder-wide EXPUNGE. Refused rather
	// than destroying mail the user did not select.
	ErrNoSafePurge = errors.New("mail: server cannot delete permanently")

	// ErrFolderExists reports that a new or moved folder would collide with one the
	// listing already has. Matched case-insensitively, like every other name here.
	ErrFolderExists = errors.New("mail: folder already exists")

	// ErrFolderNotEmpty reports a delete of a mailbox that still holds mail. The
	// refusal carries the count, so the interface can say how much is in the way.
	ErrFolderNotEmpty = errors.New("mail: folder is not empty")

	// ErrFolderHasChildren reports a delete of a mailbox that has folders below it.
	// Removing it would either orphan them or destroy them, depending on the server.
	ErrFolderHasChildren = errors.New("mail: folder has subfolders")

	// ErrFolderProtected reports a mailbox a rename or delete must not touch: INBOX,
	// or a folder an [accounts.imap] override names.
	ErrFolderProtected = errors.New("mail: folder cannot be changed")

	// ErrFolderCycle reports a move whose destination is the folder itself or one of
	// its own descendants, which would make the tree a loop.
	ErrFolderCycle = errors.New("mail: cannot move a folder into itself")

	// ErrFolderNoOp reports a move the folder is already in.
	ErrFolderNoOp = errors.New("mail: folder is already there")
)

// FolderNotEmptyError says how many messages block a delete. It matches
// ErrFolderNotEmpty, so a caller that only needs the reason can ignore the count.
type FolderNotEmptyError struct {
	Folder string
	Count  uint32
}

func (e *FolderNotEmptyError) Error() string {
	return fmt.Sprintf("%s: %q holds %s", ErrFolderNotEmpty, e.Folder, countNoun(int(e.Count), "message"))
}

// Is reports the sentinel this refusal belongs to.
func (e *FolderNotEmptyError) Is(target error) bool {
	return target == ErrFolderNotEmpty
}

// FolderHasChildrenError says how many folders sit below the one a delete named. The
// count includes deeper descendants, not just immediate children.
type FolderHasChildrenError struct {
	Folder string
	Count  int
}

func (e *FolderHasChildrenError) Error() string {
	return fmt.Sprintf("%s: %q has %s below it", ErrFolderHasChildren, e.Folder, countNoun(e.Count, "folder"))
}

// Is reports the sentinel this refusal belongs to.
func (e *FolderHasChildrenError) Is(target error) bool {
	return target == ErrFolderHasChildren
}

// countNoun renders "1 message" or "12 messages". It is the mail package's own copy
// because the interface's countLabel is not reachable from here.
func countNoun(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}

	return fmt.Sprintf("%d %ss", n, noun)
}
