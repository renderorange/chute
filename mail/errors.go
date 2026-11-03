package mail

import "errors"

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
)
