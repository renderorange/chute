package mail

import (
	"testing"

	"github.com/emersion/go-imap/v2"
)

func TestFoldersFromList(t *testing.T) {
	data := []*imap.ListData{
		{Mailbox: "Work", Delim: '/', Attrs: []imap.MailboxAttr{imap.MailboxAttrSent}},
		{Mailbox: "INBOX", Delim: '/', Attrs: []imap.MailboxAttr{imap.MailboxAttrHasNoChildren}},
		{Mailbox: "archive", Delim: '/'},
	}

	folders := foldersFromList(data)

	if len(folders) != 3 {
		t.Fatalf("folders = %d, want 3", len(folders))
	}
	if got, want := folders[0].Name, "archive"; got != want {
		t.Errorf("folders[0] = %q, want %q (case-insensitive sort)", got, want)
	}
	if got, want := folders[1].Name, "INBOX"; got != want {
		t.Errorf("folders[1] = %q, want %q", got, want)
	}
	if got, want := folders[2].Name, "Work"; got != want {
		t.Errorf("folders[2] = %q, want %q", got, want)
	}
	if got, want := folders[2].Delimiter, '/'; got != want {
		t.Errorf("delimiter = %q, want %q", got, want)
	}
}

func TestFolderRole(t *testing.T) {
	tests := []struct {
		name   string
		folder Folder
		want   Role
	}{
		{"inbox by name", Folder{Name: "INBOX"}, RoleInbox},
		{"inbox lowercase", Folder{Name: "inbox"}, RoleInbox},
		{"sent", Folder{Name: "Sent Items", Attrs: []imap.MailboxAttr{imap.MailboxAttrSent}}, RoleSent},
		{"drafts", Folder{Name: "Drafts", Attrs: []imap.MailboxAttr{imap.MailboxAttrDrafts}}, RoleDrafts},
		{"trash", Folder{Name: "Deleted", Attrs: []imap.MailboxAttr{imap.MailboxAttrTrash}}, RoleTrash},
		{"archive", Folder{Name: "Old", Attrs: []imap.MailboxAttr{imap.MailboxAttrArchive}}, RoleArchive},
		{"junk", Folder{Name: "Spam", Attrs: []imap.MailboxAttr{imap.MailboxAttrJunk}}, RoleJunk},
		{"plain folder", Folder{Name: "receipts"}, RoleNone},
		{"inbox wins over attributes", Folder{Name: "INBOX", Attrs: []imap.MailboxAttr{imap.MailboxAttrSent}}, RoleInbox},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.folder.Role(); got != tc.want {
				t.Errorf("role = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestFolderSelectable(t *testing.T) {
	container := Folder{Name: "Work", Attrs: []imap.MailboxAttr{imap.MailboxAttrNoSelect}}
	if container.Selectable() {
		t.Error("\\Noselect folder reported selectable")
	}

	regular := Folder{Name: "INBOX"}
	if !regular.Selectable() {
		t.Error("plain folder reported unselectable")
	}
}

func TestFolderPathHelpers(t *testing.T) {
	tests := []struct {
		name       string
		folder     Folder
		wantParent string
		wantLeaf   string
		wantDepth  int
	}{
		{"top level", Folder{Name: "INBOX", Delimiter: '/'}, "", "INBOX", 0},
		{"nested", Folder{Name: "Work/Projects/2026", Delimiter: '/'}, "Work/Projects", "2026", 2},
		{"no delimiter advertised", Folder{Name: "Work/Projects"}, "", "Work/Projects", 0},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.folder.Parent(); got != tc.wantParent {
				t.Errorf("parent = %q, want %q", got, tc.wantParent)
			}
			if got := tc.folder.Leaf(); got != tc.wantLeaf {
				t.Errorf("leaf = %q, want %q", got, tc.wantLeaf)
			}
			if got := tc.folder.Depth(); got != tc.wantDepth {
				t.Errorf("depth = %d, want %d", got, tc.wantDepth)
			}
		})
	}
}

func TestFilterFolders(t *testing.T) {
	folders := []Folder{
		{Name: "INBOX", Delimiter: '/'},
		{Name: "Notes", Delimiter: '/'},
		{Name: "Notes/Archive", Delimiter: '/'},
		{Name: "Work", Delimiter: '/'},
	}

	got := FilterFolders(folders, []string{"notes"})
	if len(got) != 2 {
		t.Fatalf("folders = %d, want 2", len(got))
	}
	if got[0].Name != "INBOX" || got[1].Name != "Work" {
		t.Errorf("folders = %v, want INBOX and Work", got)
	}

	if same := FilterFolders(folders, nil); len(same) != len(folders) {
		t.Errorf("empty hide list removed folders: %v", same)
	}
}
