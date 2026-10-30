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

// defaultLoadFolders resolves the password, connects, lists folders, and disconnects.
func defaultLoadFolders(acct config.Account) ([]mail.Folder, error) {
	client, err := connect(acct)
	if err != nil {
		return nil, err
	}
	defer client.Close()

	return client.Folders()
}

// defaultLoadMessages resolves the password, connects, fetches envelopes, and
// disconnects. M2a reconnects per folder; a persistent client arrives with the
// per-account goroutine.
func defaultLoadMessages(acct config.Account, folder string) ([]mail.Message, error) {
	client, err := connect(acct)
	if err != nil {
		return nil, err
	}
	defer client.Close()

	return client.FetchEnvelopes(folder)
}

// defaultLoadBody resolves the password, connects, fetches one body, and
// disconnects. M2b reconnects per opened message; the persistent client arrives
// with the per-account goroutine.
func defaultLoadBody(acct config.Account, folder string, uid imap.UID) (*mail.Body, error) {
	client, err := connect(acct)
	if err != nil {
		return nil, err
	}
	defer client.Close()

	return client.FetchBody(folder, uid)
}

// connect resolves the account password and opens an IMAP connection.
func connect(acct config.Account) (*mail.Client, error) {
	password, err := acct.IMAPPassword()
	if err != nil {
		return nil, err
	}

	return mail.Connect(acct, password)
}
