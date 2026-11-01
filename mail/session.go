package mail

import (
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/emersion/go-imap/v2"
	"github.com/renderorange/chute/config"
)

// errSessionClosed is returned when a session is used after Close.
var errSessionClosed = errors.New("mail: session is closed")

// ErrAlreadyInTrash reports that a delete was asked for inside the trash mailbox itself.
// It is typed so the interface can say "already in Trash" instead of echoing the wrapped
// account context at the user.
var ErrAlreadyInTrash = errors.New("mail: already the trash folder")

// ErrNoRoleFolder reports that no mailbox serves a role and no override names one.
// Callers match it with errors.Is to distinguish "cannot file a copy" from a real
// protocol failure.
var ErrNoRoleFolder = errors.New("mail: no mailbox serves the role")

// sessionClient is the connected-client surface a session reuses.
type sessionClient interface {
	Folders() ([]Folder, error)
	FetchEnvelopes(folder string) ([]Message, error)
	FetchBody(folder string, uid imap.UID) (*Body, error)
	FetchCompose(folder string, uid imap.UID) (*Compose, error)
	Append(folder string, flags []imap.Flag, msg []byte) (imap.UID, bool, error)
	StoreFlags(folder string, uids []imap.UID, add bool, flags ...imap.Flag) error
	DeleteMessages(folder, trash string, uids []imap.UID) error
	Close() error
}

// connectFunc opens a client for an account.
type connectFunc func(acct config.Account) (sessionClient, error)

// Session is one account's persistent connection. It connects on first use,
// reuses the connection for later operations, and drops it after a failure so
// the next operation reconnects. The mutex serializes operations on the one
// underlying connection; the UI never blocks because every call runs in a
// bubbletea command goroutine.
type Session struct {
	mu      sync.Mutex
	acct    config.Account
	connect connectFunc
	client  sessionClient
	closed  bool

	// folders is the last unfiltered listing. Hiding a mailbox with
	// hide_folders must not make its role unresolvable, so delete still works
	// when the user hides Trash.
	folders []Folder
}

// newSession builds a session that connects through connect when first used.
func newSession(acct config.Account, connect connectFunc) *Session {
	return &Session{acct: acct, connect: connect}
}

// Folders lists the account's mailboxes over the persistent connection.
func (s *Session) Folders() ([]Folder, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	client, err := s.acquireLocked()
	if err != nil {
		return nil, err
	}

	folders, err := client.Folders()
	if err != nil {
		s.resetLocked()
		return nil, err
	}

	s.folders = folders

	return FilterFolders(folders, s.acct.IMAP.HideFolders), nil
}

// RoleFolder resolves the mailbox that serves a role, using the most recent
// folder listing and the account's [accounts.imap] overrides. It never connects:
// every action that needs a role runs with a folder open, which means a listing
// has already happened.
func (s *Session) RoleFolder(role Role) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	folder, ok := s.roleFolderLocked(role)
	if !ok {
		return "", false
	}

	return folder.Name, true
}

// roleFolderLocked resolves a role against the cached listing.
func (s *Session) roleFolderLocked(role Role) (Folder, bool) {
	return ResolveFolder(s.folders, role, roleOverride(s.acct, role))
}

// Delete moves messages to the account's trash mailbox, resolved from the last
// folder listing. Deleting from the trash itself is refused: permanent delete is
// not part of this milestone.
func (s *Session) Delete(folder string, uids []imap.UID) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	trash, ok := s.roleFolderLocked(RoleTrash)
	if !ok {
		return fmt.Errorf("mail: account %q: no mailbox serves the trash role; set [accounts.imap] trash", s.acct.Name)
	}
	if strings.EqualFold(folder, trash.Name) {
		return fmt.Errorf("%w: account %q: %q", ErrAlreadyInTrash, s.acct.Name, folder)
	}

	client, err := s.acquireLocked()
	if err != nil {
		return err
	}

	if err := client.DeleteMessages(folder, trash.Name, uids); err != nil {
		s.resetLocked()
		return err
	}

	return nil
}

// roleOverride maps a role onto the account's configured mailbox override. The
// inbox has none; StartFolder covers that case.
func roleOverride(acct config.Account, role Role) string {
	switch role {
	case RoleSent:
		return acct.IMAP.Sent
	case RoleDrafts:
		return acct.IMAP.Drafts
	case RoleTrash:
		return acct.IMAP.Trash
	case RoleArchive:
		return acct.IMAP.Archive
	case RoleJunk:
		return acct.IMAP.Junk
	}

	return ""
}

// FetchEnvelopes loads one folder's message envelopes over the persistent
// connection.
func (s *Session) FetchEnvelopes(folder string) ([]Message, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	client, err := s.acquireLocked()
	if err != nil {
		return nil, err
	}

	messages, err := client.FetchEnvelopes(folder)
	if err != nil {
		s.resetLocked()
		return nil, err
	}

	return messages, nil
}

// FetchBody loads one message body over the persistent connection.
func (s *Session) FetchBody(folder string, uid imap.UID) (*Body, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	client, err := s.acquireLocked()
	if err != nil {
		return nil, err
	}

	body, err := client.FetchBody(folder, uid)
	if err != nil {
		s.resetLocked()
		return nil, err
	}

	return body, nil
}

// StoreFlags adds or removes flags on every listed message in one command.
func (s *Session) StoreFlags(folder string, uids []imap.UID, add bool, flags ...imap.Flag) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	client, err := s.acquireLocked()
	if err != nil {
		return err
	}

	if err := client.StoreFlags(folder, uids, add, flags...); err != nil {
		s.resetLocked()
		return err
	}

	return nil
}

// FetchCompose loads one message as an outgoing message, so a draft reopens in the
// composer.
func (s *Session) FetchCompose(folder string, uid imap.UID) (*Compose, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	client, err := s.acquireLocked()
	if err != nil {
		return nil, err
	}

	compose, err := client.FetchCompose(folder, uid)
	if err != nil {
		s.resetLocked()
		return nil, err
	}

	return compose, nil
}

// SaveDraft appends msg to folder as a draft. The boolean reports whether the server
// named the new UID; without UIDPLUS it cannot, and the caller then has no way to
// remove that draft later.
func (s *Session) SaveDraft(folder string, msg []byte) (imap.UID, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if strings.TrimSpace(folder) == "" {
		return 0, false, fmt.Errorf("mail: account %q: no drafts mailbox is configured", s.acct.Name)
	}

	client, err := s.acquireLocked()
	if err != nil {
		return 0, false, err
	}

	uid, known, err := client.Append(folder, []imap.Flag{imap.FlagDraft, imap.FlagSeen}, msg)
	if err != nil {
		s.resetLocked()
		return 0, false, err
	}

	return uid, known, nil
}

// AppendSent files a copy of msg in the account's sent mailbox. It resolves the role
// itself and returns ErrNoRoleFolder when neither an override nor the \Sent attribute
// names one, so the caller can send anyway and say the copy was not filed.
func (s *Session) AppendSent(msg []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	sent, ok := s.roleFolderLocked(RoleSent)
	if !ok {
		return fmt.Errorf("%w: account %q: set [accounts.imap] sent", ErrNoRoleFolder, s.acct.Name)
	}

	client, err := s.acquireLocked()
	if err != nil {
		return err
	}

	if _, _, err := client.Append(sent.Name, []imap.Flag{imap.FlagSeen}, msg); err != nil {
		s.resetLocked()
		return err
	}

	return nil
}

// DraftsFolder resolves the drafts mailbox from the last folder listing.
func (s *Session) DraftsFolder() (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	folder, ok := s.roleFolderLocked(RoleDrafts)
	if !ok {
		return "", false
	}

	return folder.Name, true
}

// Close logs out and marks the session unusable.
func (s *Session) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.closed = true
	if s.client == nil {
		return nil
	}

	err := s.client.Close()
	s.client = nil

	return err
}

// acquireLocked returns the live client, connecting if there is none.
func (s *Session) acquireLocked() (sessionClient, error) {
	if s.closed {
		return nil, errSessionClosed
	}
	if s.client != nil {
		return s.client, nil
	}

	client, err := s.connect(s.acct)
	if err != nil {
		return nil, err
	}
	s.client = client

	return client, nil
}

// resetLocked drops a failed connection so the next operation reconnects.
func (s *Session) resetLocked() {
	if s.client == nil {
		return
	}

	s.client.Close()
	s.client = nil
}

// defaultConnect resolves the account password and opens an IMAP connection.
func defaultConnect(acct config.Account, opts ...Option) (sessionClient, error) {
	password, err := acct.IMAPPassword()
	if err != nil {
		return nil, err
	}

	client, err := Connect(acct, password, opts...)
	if err != nil {
		return nil, err
	}

	return client, nil
}

// Manager owns one persistent session per account, keyed by account name.
type Manager struct {
	mu       sync.Mutex
	connect  connectFunc
	sessions map[string]*Session
	closed   bool
}

// NewManager builds a manager that connects accounts on first use. Connection
// options, such as a debug writer, apply to every account it connects.
func NewManager(opts ...Option) *Manager {
	return &Manager{
		connect: func(acct config.Account) (sessionClient, error) {
			return defaultConnect(acct, opts...)
		},
		sessions: make(map[string]*Session),
	}
}

// For returns the account's session, creating it on first use.
func (m *Manager) For(acct config.Account) *Session {
	m.mu.Lock()
	defer m.mu.Unlock()

	if session, ok := m.sessions[acct.Name]; ok {
		return session
	}

	session := newSession(acct, m.connect)
	if m.closed {
		session.closed = true
	}
	m.sessions[acct.Name] = session

	return session
}

// Close logs out every account connection.
func (m *Manager) Close() error {
	m.mu.Lock()
	m.closed = true
	sessions := make([]*Session, 0, len(m.sessions))
	for _, session := range m.sessions {
		sessions = append(sessions, session)
	}
	m.mu.Unlock()

	var firstErr error
	for _, session := range sessions {
		if err := session.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}

	return firstErr
}
