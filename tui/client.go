package tui

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/emersion/go-imap/v2"
	"github.com/renderorange/chute/config"
	"github.com/renderorange/chute/mail"
)

// loadFoldersFunc connects to an account and returns its folders. Tests replace it
// with a fake so the model can be exercised without a server.
type loadFoldersFunc func(acct config.Account) ([]mail.Folder, error)

// loadMessagesFunc opens a folder and returns its message envelopes.
type loadMessagesFunc func(acct config.Account, folder string) ([]mail.Message, error)

// loadBodyFunc downloads one message body and its attachments.
type loadBodyFunc func(acct config.Account, folder string, uid imap.UID) (*mail.Body, error)

// storeFlagsFunc adds or removes flags on a set of messages in one command.
type storeFlagsFunc func(acct config.Account, folder string, uids []imap.UID, add bool, flags ...imap.Flag) error

// deleteMessagesFunc moves messages to the account's trash mailbox.
type deleteMessagesFunc func(acct config.Account, folder string, uids []imap.UID) error

// moveToRoleFunc files messages under the mailbox that serves a role (archive or
// junk), returning the destination's name.
type moveToRoleFunc func(acct config.Account, folder string, role mail.Role, uids []imap.UID) (string, error)

// moveToFolderFunc files messages under a mailbox the user names.
type moveToFolderFunc func(acct config.Account, folder, dest string, uids []imap.UID) error

// fetchComposeFunc reads one message as an editable draft.
type fetchComposeFunc func(acct config.Account, folder string, uid imap.UID) (*mail.Compose, error)

// saveDraftFunc appends a message to the account's drafts mailbox.
type saveDraftFunc func(acct config.Account, folder string, msg []byte) (imap.UID, bool, error)

// appendSentFunc files a copy of a sent message in the account's sent mailbox.
type appendSentFunc func(acct config.Account, msg []byte) error

// sendMessageFunc delivers a message over SMTP.
type sendMessageFunc func(acct config.Account, msg []byte) error

// draftsFolderFunc resolves the account's drafts mailbox from its last listing.
type draftsFolderFunc func(acct config.Account) (string, bool)

// unseenFunc counts the unseen messages in one folder of one account.
type unseenFunc func(acct config.Account, folder string) (uint32, error)

// pollFunc runs one incremental new-mail check for a folder.
type pollFunc func(acct config.Account, folder string, since imap.UID) (mail.PollResult, error)

// uidsFunc lists every message UID in a folder, for reconciling a deletion made
// by another client.
type uidsFunc func(acct config.Account, folder string) ([]imap.UID, error)

// tickFunc arms a one-shot timer. It exists so a test can observe the interval and
// fire the tick without waiting for it.
type tickFunc func(d time.Duration, fn func(time.Time) tea.Msg) tea.Cmd

// loaders groups the background loaders the model calls.
type loaders struct {
	folders        loadFoldersFunc
	messages       loadMessagesFunc
	body           loadBodyFunc
	storeFlags     storeFlagsFunc
	deleteMessages deleteMessagesFunc
	moveToRole     moveToRoleFunc
	moveToFolder   moveToFolderFunc

	fetchCompose     fetchComposeFunc
	saveDraft        saveDraftFunc
	appendSent       appendSentFunc
	sendMessage      sendMessageFunc
	findDraftsFolder draftsFolderFunc
	countUnseen      unseenFunc
	pollFolder       pollFunc
	countUIDs        uidsFunc
}

// sessionLoaders binds every loader to one persistent session per account, so
// opening a folder, listing envelopes and reading a body reuse one connection.
func sessionLoaders(manager *mail.Manager) loaders {
	return loaders{
		folders: func(acct config.Account) ([]mail.Folder, error) {
			return manager.For(acct).Folders()
		},
		messages: func(acct config.Account, folder string) ([]mail.Message, error) {
			return manager.For(acct).FetchEnvelopes(folder)
		},
		body: func(acct config.Account, folder string, uid imap.UID) (*mail.Body, error) {
			return manager.For(acct).FetchBody(folder, uid)
		},
		storeFlags: func(acct config.Account, folder string, uids []imap.UID, add bool, flags ...imap.Flag) error {
			return manager.For(acct).StoreFlags(folder, uids, add, flags...)
		},
		deleteMessages: func(acct config.Account, folder string, uids []imap.UID) error {
			return manager.For(acct).Delete(folder, uids)
		},
		moveToRole: func(acct config.Account, folder string, role mail.Role, uids []imap.UID) (string, error) {
			return manager.For(acct).MoveTo(folder, role, uids)
		},
		moveToFolder: func(acct config.Account, folder, dest string, uids []imap.UID) error {
			return manager.For(acct).MoveToFolder(folder, dest, uids)
		},
		fetchCompose: func(acct config.Account, folder string, uid imap.UID) (*mail.Compose, error) {
			return manager.For(acct).FetchCompose(folder, uid)
		},
		saveDraft: func(acct config.Account, folder string, msg []byte) (imap.UID, bool, error) {
			return manager.For(acct).SaveDraft(folder, msg)
		},
		appendSent: func(acct config.Account, msg []byte) error {
			return manager.For(acct).AppendSent(msg)
		},
		sendMessage: func(acct config.Account, msg []byte) error {
			password, err := acct.SMTPPassword()
			if err != nil {
				return err
			}

			return mail.SendMessage(acct, password, msg)
		},
		findDraftsFolder: func(acct config.Account) (string, bool) {
			return manager.For(acct).DraftsFolder()
		},
		countUnseen: func(acct config.Account, folder string) (uint32, error) {
			return manager.For(acct).Unseen(folder)
		},
		pollFolder: func(acct config.Account, folder string, since imap.UID) (mail.PollResult, error) {
			return manager.For(acct).Poll(folder, since)
		},
		countUIDs: func(acct config.Account, folder string) ([]imap.UID, error) {
			return manager.For(acct).UIDs(folder)
		},
	}
}

// foldersMsg carries the result of loading folders for one account.
type foldersMsg struct {
	account string
	folders []mail.Folder
	err     error
}

// messagesMsg carries the result of loading one folder's messages.
type messagesMsg struct {
	account  string
	folder   string
	messages []mail.Message
	err      error
}

// bodyKey identifies a cached body within one account and folder.
type bodyKey struct {
	account string
	folder  string
	uid     imap.UID
}

// bodyMsg carries the result of loading one message body.
type bodyMsg struct {
	account string
	folder  string
	uid     imap.UID
	body    *mail.Body
	err     error
}

// flagsMsg carries the result of one flag change, so the model can update the
// rows the server confirmed. quiet marks a change the user did not ask for.
type flagsMsg struct {
	account string
	folder  string
	uids    []imap.UID
	add     bool
	flags   []imap.Flag
	quiet   bool
	err     error
}

// deleteMsg carries the result of moving messages to the trash mailbox.
type deleteMsg struct {
	account string
	folder  string
	uids    []imap.UID
	err     error
}

// moveMsg carries the result of archiving or junking messages. dest is the mailbox
// the session resolved, which the status bar names.
type moveMsg struct {
	account string
	folder  string
	role    mail.Role
	dest    string
	uids    []imap.UID
	err     error
}

// unseenMsg carries one account's unread count for the account selector. Each
// account reports separately so an unreachable server delays only its own row.
type unseenMsg struct {
	account string
	count   uint32
	err     error
}

// folderKey identifies one folder of one account.
type folderKey struct {
	account string
	folder  string
}

// pollTickMsg asks for one new-mail check.
type pollTickMsg struct{}

// pollMsg carries the result of one incremental check.
type pollMsg struct {
	account string
	folder  string
	result  mail.PollResult
	err     error
}

// uidsMsg carries every surviving UID in a folder, after a poll found the message
// count had dropped because another client removed mail.
type uidsMsg struct {
	account string
	folder  string
	uids    []imap.UID
	err     error
}
