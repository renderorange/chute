// Package mail talks to IMAP servers.
package mail

import (
	"crypto/tls"
	"fmt"
	"sort"
	"strings"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
	"github.com/renderorange/chute/config"
)

// Client is a connected IMAP account.
type Client struct {
	account string
	conn    *imapclient.Client
}

// Connect opens a TLS connection and logs in. The password is used once and is
// never retained.
func Connect(acct config.Account, password string) (*Client, error) {
	options := &imapclient.Options{
		TLSConfig: &tls.Config{ServerName: acct.IMAP.Host},
	}

	conn, err := imapclient.DialTLS(acct.IMAPAddress(), options)
	if err != nil {
		return nil, fmt.Errorf("mail: account %q: connecting to %s: %w", acct.Name, acct.IMAPAddress(), err)
	}

	if err := conn.Login(acct.IMAP.Username, password).Wait(); err != nil {
		conn.Close()
		return nil, fmt.Errorf("mail: account %q: login as %s failed: %w", acct.Name, acct.IMAP.Username, err)
	}

	return &Client{account: acct.Name, conn: conn}, nil
}

// Close logs out and drops the connection. A failing logout still closes.
func (c *Client) Close() error {
	err := c.conn.Logout().Wait()
	if closeErr := c.conn.Close(); err == nil {
		err = closeErr
	}

	return err
}

// Folders lists every mailbox on the account, sorted by name.
func (c *Client) Folders() ([]Folder, error) {
	cmd := c.conn.List("", "*", nil)
	defer cmd.Close()

	data, err := cmd.Collect()
	if err != nil {
		return nil, fmt.Errorf("mail: account %q: listing folders: %w", c.account, err)
	}

	return foldersFromList(data), nil
}

// foldersFromList converts protocol data into sorted folders.
func foldersFromList(data []*imap.ListData) []Folder {
	folders := make([]Folder, 0, len(data))

	for _, item := range data {
		folders = append(folders, Folder{
			Name:      item.Mailbox,
			Delimiter: item.Delim,
			Attrs:     item.Attrs,
		})
	}

	sort.Slice(folders, func(i, j int) bool {
		return strings.ToLower(folders[i].Name) < strings.ToLower(folders[j].Name)
	})

	return folders
}
