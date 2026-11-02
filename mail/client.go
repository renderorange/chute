// Package mail talks to IMAP servers and decodes message bodies.
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
	caps    imap.CapSet
}

// Connect opens a TLS connection and logs in. The password is used once and is
// retained only to mask itself in the debug stream, if one was requested.
func Connect(acct config.Account, password string, opts ...Option) (*Client, error) {
	return connectWith(dialTLS, acct, password, opts...)
}

// dialFunc opens a protocol connection for config tests and for the in-process
// server the suite runs against.
type dialFunc func(address string, options *imapclient.Options) (*imapclient.Client, error)

// dialTLS is the production dialer.
func dialTLS(address string, options *imapclient.Options) (*imapclient.Client, error) {
	return imapclient.DialTLS(address, options)
}

// connectWith opens a connection through dial, logs in and caches the
// post-login capabilities. Pre-auth capabilities are not authoritative, so the
// set is fetched again after login; MOVE and UIDPLUS decide how delete runs.
func connectWith(dial dialFunc, acct config.Account, password string, opts ...Option) (*Client, error) {
	connOpts := newConnectOptions(opts)

	if connOpts.dial != nil {
		dial = connOpts.dial
	}

	options := &imapclient.Options{
		TLSConfig: &tls.Config{ServerName: acct.IMAP.Host},
	}

	if connOpts.debugWriter != nil {
		options.DebugWriter = newRedactWriter(connOpts.debugWriter, password)
	}

	conn, err := dial(acct.IMAPAddress(), options)
	if err != nil {
		return nil, fmt.Errorf("mail: account %q: connecting to %s: %w", acct.Name, acct.IMAPAddress(), err)
	}

	if err := conn.Login(acct.IMAP.Username, password).Wait(); err != nil {
		conn.Close()
		return nil, fmt.Errorf("mail: account %q: login as %s failed: %w", acct.Name, acct.IMAP.Username, err)
	}

	caps, capsErr := conn.Capability().Wait()
	if capsErr != nil {
		// A server that cannot answer CAPABILITY still serves mail. Leaving the
		// set empty makes delete refuse loudly later instead of failing the login.
		caps = nil
	}

	return &Client{account: acct.Name, conn: conn, caps: caps}, nil
}

// Supports reports whether the server advertised a capability after login.
func (c *Client) Supports(cap imap.Cap) bool {
	return c.caps.Has(cap)
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

// FetchEnvelopes opens a folder and returns every message's envelope, flags and
// size. No message body is fetched.
func (c *Client) FetchEnvelopes(folder string) ([]Message, error) {
	if _, err := c.conn.Select(folder, nil).Wait(); err != nil {
		return nil, fmt.Errorf("mail: account %q: opening %q: %w", c.account, folder, err)
	}

	cmd := c.conn.Fetch(allMessages(), &imap.FetchOptions{
		Envelope:     true,
		Flags:        true,
		InternalDate: true,
		RFC822Size:   true,
		UID:          true,
	})
	defer cmd.Close()

	buffers, err := cmd.Collect()
	if err != nil {
		return nil, fmt.Errorf("mail: account %q: fetching envelopes from %q: %w", c.account, folder, err)
	}

	return messagesFromBuffers(buffers), nil
}

// PollResult is what one incremental check found in a folder.
type PollResult struct {
	// Messages holds only the envelopes whose UID is greater than the high-water
	// mark the caller passed in.
	Messages []Message

	// Total is the server's message count for the folder, used to notice that
	// another client removed something.
	Total uint32

	// Unseen is the server's unread count for the folder.
	Unseen uint32
}

// Poll runs one incremental new-mail check: SELECT for the message count, a
// UID FETCH of everything after since, and a STATUS for the unread count. It
// fetches no body and no envelope the caller already has.
func (c *Client) Poll(folder string, since imap.UID) (PollResult, error) {
	selectData, err := c.conn.Select(folder, nil).Wait()
	if err != nil {
		return PollResult{}, fmt.Errorf("mail: account %q: opening %q: %w", c.account, folder, err)
	}

	result := PollResult{Total: selectData.NumMessages}

	if selectData.NumMessages > 0 {
		messages, err := c.fetchAfter(folder, since)
		if err != nil {
			return PollResult{}, err
		}
		result.Messages = messages
	}

	status, err := c.conn.Status(folder, &imap.StatusOptions{NumUnseen: true}).Wait()
	if err != nil {
		return PollResult{}, fmt.Errorf("mail: account %q: counting unread in %q: %w", c.account, folder, err)
	}
	if status.NumUnseen != nil {
		result.Unseen = *status.NumUnseen
	}

	return result, nil
}

// fetchAfter runs the incremental FETCH and keeps only the messages newer than
// since. UID FETCH n:* also returns the highest-UID message when n exceeds it, so
// without the filter a quiet poll would re-announce the last message as new.
func (c *Client) fetchAfter(folder string, since imap.UID) ([]Message, error) {
	var set imap.UIDSet
	set.AddRange(since+1, 0)

	cmd := c.conn.Fetch(set, &imap.FetchOptions{
		Envelope:     true,
		Flags:        true,
		InternalDate: true,
		RFC822Size:   true,
		UID:          true,
	})
	buffers, err := cmd.Collect()
	cmd.Close()

	if err != nil {
		return nil, fmt.Errorf("mail: account %q: polling %q: %w", c.account, folder, err)
	}

	fresh := make([]Message, 0, len(buffers))
	for _, msg := range messagesFromBuffers(buffers) {
		if msg.UID > since {
			fresh = append(fresh, msg)
		}
	}

	return fresh, nil
}

// UIDs lists every message UID in a folder, with no envelope. A poll uses it after
// the server's message count drops, to learn which rows another client removed.
func (c *Client) UIDs(folder string) ([]imap.UID, error) {
	if _, err := c.conn.Select(folder, nil).Wait(); err != nil {
		return nil, fmt.Errorf("mail: account %q: opening %q: %w", c.account, folder, err)
	}

	cmd := c.conn.Fetch(allMessages(), &imap.FetchOptions{UID: true})
	defer cmd.Close()

	buffers, err := cmd.Collect()
	if err != nil {
		return nil, fmt.Errorf("mail: account %q: listing UIDs in %q: %w", c.account, folder, err)
	}

	uids := make([]imap.UID, 0, len(buffers))
	for _, buf := range buffers {
		uids = append(uids, buf.UID)
	}

	return uids, nil
}

// Unseen counts the messages in folder that do not carry \Seen, with one STATUS
// command. A server that omits the number is reported as zero: the count is a hint
// in the account selector, and a missing number is not worth failing over.
func (c *Client) Unseen(folder string) (uint32, error) {
	data, err := c.conn.Status(folder, &imap.StatusOptions{NumUnseen: true}).Wait()
	if err != nil {
		return 0, fmt.Errorf("mail: account %q: counting unread in %q: %w", c.account, folder, err)
	}
	if data.NumUnseen == nil {
		return 0, nil
	}

	return *data.NumUnseen, nil
}

// allMessages is the sequence set covering an entire folder. It must be a value
// SeqSet: go-imap's imapwire.NumSetKind type-switches on the value type and
// panics on a *imap.SeqSet, which crashed the UI when a folder opened.
func allMessages() imap.NumSet {
	var seqset imap.SeqSet
	seqset.AddRange(1, 0)

	return seqset
}

// uidSet builds a value UIDSet for the same reason: a pointer panics in
// imapwire.NumSetKind.
func uidSet(uids []imap.UID) imap.UIDSet {
	var set imap.UIDSet
	for _, uid := range uids {
		set.AddNum(uid)
	}

	return set
}

// StoreFlags adds or removes flags on every listed message in one command, so a
// bulk action is one round trip. Selecting the folder first is required by the
// protocol; an empty selection is a no-op.
func (c *Client) StoreFlags(folder string, uids []imap.UID, add bool, flags ...imap.Flag) error {
	if len(uids) == 0 {
		return nil
	}

	if _, err := c.conn.Select(folder, nil).Wait(); err != nil {
		return fmt.Errorf("mail: account %q: opening %q: %w", c.account, folder, err)
	}

	op := imap.StoreFlagsDel
	if add {
		op = imap.StoreFlagsAdd
	}

	cmd := c.conn.Store(uidSet(uids), &imap.StoreFlags{Op: op, Silent: true, Flags: flags}, nil)

	if err := cmd.Close(); err != nil {
		return fmt.Errorf("mail: account %q: updating flags in %q: %w", c.account, folder, err)
	}

	return nil
}

// MoveMessages moves every listed message to dest in one command. An empty
// selection is a no-op.
func (c *Client) MoveMessages(folder, dest string, uids []imap.UID) error {
	if len(uids) == 0 {
		return nil
	}
	if dest == "" {
		return fmt.Errorf("mail: account %q: moving from %q needs a destination folder", c.account, folder)
	}
	if strings.EqualFold(dest, folder) {
		return fmt.Errorf("mail: account %q: moving %q messages to %q would be a no-op", c.account, folder, dest)
	}

	if _, err := c.conn.Select(folder, nil).Wait(); err != nil {
		return fmt.Errorf("mail: account %q: opening %q: %w", c.account, folder, err)
	}

	if _, err := c.conn.Move(uidSet(uids), dest).Wait(); err != nil {
		return fmt.Errorf("mail: account %q: moving messages from %q to %q: %w", c.account, folder, dest, err)
	}

	return nil
}

// MoveToFolder moves messages to dest using the same policy as delete: MOVE when
// the server has it, COPY + \Deleted + UID EXPUNGE when it has UIDPLUS, and a
// refusal when it has neither. It never issues a folder-wide EXPUNGE, which would
// destroy messages the user did not select.
//
// Archive and junk are this move with a different destination, so they share it
// instead of growing a second protocol choice.
func (c *Client) MoveToFolder(folder, dest string, uids []imap.UID) error {
	if len(uids) == 0 {
		return nil
	}
	if dest == "" {
		return fmt.Errorf("mail: account %q: moving from %q needs a destination folder", c.account, folder)
	}
	// The fallback copies to dest and then expunges the source UIDs, so a
	// destination equal to the source would copy every message onto itself and then
	// delete it for good. Refusing here protects against that even if a caller
	// skips its own guard.
	if strings.EqualFold(dest, folder) {
		return fmt.Errorf("mail: account %q: %q is the source folder; refusing to move it into itself", c.account, folder)
	}

	switch {
	case c.Supports(imap.CapMove):
		return c.MoveMessages(folder, dest, uids)

	case c.Supports(imap.CapUIDPlus):
		return c.copyAndPurge(folder, dest, uids)
	}

	return fmt.Errorf("mail: account %q: moving from %q needs MOVE or UIDPLUS; the server advertises neither", c.account, folder)
}

// DeleteMessages moves messages to the trash mailbox through MoveToFolder, so the
// policy exists once.
func (c *Client) DeleteMessages(folder, trash string, uids []imap.UID) error {
	return c.MoveToFolder(folder, trash, uids)
}

// copyAndPurge deletes without the MOVE extension: copy to Trash, mark the copies
// \Deleted in the source, then expunge exactly those UIDs.
func (c *Client) copyAndPurge(folder, dest string, uids []imap.UID) error {
	if _, err := c.conn.Select(folder, nil).Wait(); err != nil {
		return fmt.Errorf("mail: account %q: opening %q: %w", c.account, folder, err)
	}

	if _, err := c.conn.Copy(uidSet(uids), dest).Wait(); err != nil {
		return fmt.Errorf("mail: account %q: copying messages from %q to %q: %w", c.account, folder, dest, err)
	}

	cmd := c.conn.Store(uidSet(uids), &imap.StoreFlags{
		Op:     imap.StoreFlagsAdd,
		Silent: true,
		Flags:  []imap.Flag{imap.FlagDeleted},
	}, nil)
	if err := cmd.Close(); err != nil {
		return fmt.Errorf("mail: account %q: marking messages deleted in %q: %w", c.account, folder, err)
	}

	if err := c.conn.UIDExpunge(uidSet(uids)).Close(); err != nil {
		return fmt.Errorf("mail: account %q: expunging deleted messages in %q: %w", c.account, folder, err)
	}

	return nil
}

// messagesFromBuffers converts protocol data into messages, in server order.
func messagesFromBuffers(buffers []*imapclient.FetchMessageBuffer) []Message {
	messages := make([]Message, 0, len(buffers))

	for _, buf := range buffers {
		msg := Message{
			UID:          buf.UID,
			SeqNum:       buf.SeqNum,
			Flags:        buf.Flags,
			Size:         buf.RFC822Size,
			InternalDate: buf.InternalDate,
		}
		if buf.Envelope != nil {
			msg.Envelope = *buf.Envelope
			msg.MessageID = buf.Envelope.MessageID
			msg.InReplyTo = buf.Envelope.InReplyTo
		}

		messages = append(messages, msg)
	}

	return messages
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
