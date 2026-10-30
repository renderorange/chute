package mail

import (
	"strings"

	"github.com/emersion/go-imap/v2"
)

// Role is what a folder is for, independent of what the server calls it.
type Role string

const (
	RoleNone    Role = ""
	RoleInbox   Role = "inbox"
	RoleSent    Role = "sent"
	RoleDrafts  Role = "drafts"
	RoleTrash   Role = "trash"
	RoleArchive Role = "archive"
	RoleJunk    Role = "junk"
)

// Folder is one mailbox on the server.
type Folder struct {
	Name      string
	Delimiter rune
	Attrs     []imap.MailboxAttr
}

// HasAttr reports whether the server advertised an attribute for this folder.
func (f Folder) HasAttr(attr imap.MailboxAttr) bool {
	for _, have := range f.Attrs {
		if have == attr {
			return true
		}
	}

	return false
}

// Role maps the server's special-use attributes onto a stable role. INBOX has no
// special-use attribute, so it is matched by name.
func (f Folder) Role() Role {
	switch {
	case strings.EqualFold(f.Name, "INBOX"):
		return RoleInbox
	case f.HasAttr(imap.MailboxAttrSent):
		return RoleSent
	case f.HasAttr(imap.MailboxAttrDrafts):
		return RoleDrafts
	case f.HasAttr(imap.MailboxAttrTrash):
		return RoleTrash
	case f.HasAttr(imap.MailboxAttrArchive):
		return RoleArchive
	case f.HasAttr(imap.MailboxAttrJunk):
		return RoleJunk
	}

	return RoleNone
}

// Selectable reports whether the folder holds messages. Containers such as
// "[Gmail]" advertise \Noselect and cannot be opened.
func (f Folder) Selectable() bool {
	return !f.HasAttr(imap.MailboxAttrNoSelect)
}

// Parent returns the folder one level up, or an empty string at the top.
func (f Folder) Parent() string {
	if f.Delimiter == 0 {
		return ""
	}

	idx := strings.LastIndex(f.Name, string(f.Delimiter))
	if idx < 0 {
		return ""
	}

	return f.Name[:idx]
}

// Depth counts how many delimiter levels a folder sits below the top.
func (f Folder) Depth() int {
	if f.Delimiter == 0 {
		return 0
	}

	return strings.Count(f.Name, string(f.Delimiter))
}

// Leaf returns the last element of the folder path.
func (f Folder) Leaf() string {
	if f.Delimiter == 0 {
		return f.Name
	}

	idx := strings.LastIndex(f.Name, string(f.Delimiter))
	if idx < 0 {
		return f.Name
	}

	return f.Name[idx+1:]
}
