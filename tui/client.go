package tui

import (
	"github.com/renderorange/chute/config"
	"github.com/renderorange/chute/mail"
)

// loadFoldersFunc connects to an account and returns its folders. Tests replace it
// with a fake so the model can be exercised without a server.
type loadFoldersFunc func(acct config.Account) ([]mail.Folder, error)

// foldersMsg carries the result of loading folders for one account.
type foldersMsg struct {
	account string
	folders []mail.Folder
	err     error
}

// defaultLoadFolders resolves the password, connects, lists folders, and disconnects.
//
// M1 closes the connection after listing; M2 keeps it open for message fetches.
func defaultLoadFolders(acct config.Account) ([]mail.Folder, error) {
	password, err := acct.IMAPPassword()
	if err != nil {
		return nil, err
	}

	client, err := mail.Connect(acct, password)
	if err != nil {
		return nil, err
	}
	defer client.Close()

	return client.Folders()
}
