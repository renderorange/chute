package tui

import (
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

// loaders groups the background loaders the model calls.
type loaders struct {
	folders        loadFoldersFunc
	messages       loadMessagesFunc
	body           loadBodyFunc
	storeFlags     storeFlagsFunc
	deleteMessages deleteMessagesFunc
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
// rows the server confirmed.
type flagsMsg struct {
	account string
	folder  string
	uids    []imap.UID
	add     bool
	flags   []imap.Flag
	err     error
}

// deleteMsg carries the result of moving messages to the trash mailbox.
type deleteMsg struct {
	account string
	folder  string
	uids    []imap.UID
	err     error
}
