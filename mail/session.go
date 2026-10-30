package mail

import (
	"errors"
	"sync"

	"github.com/emersion/go-imap/v2"
	"github.com/renderorange/chute/config"
)

// errSessionClosed is returned when a session is used after Close.
var errSessionClosed = errors.New("mail: session is closed")

// sessionClient is the connected-client surface a session reuses.
type sessionClient interface {
	Folders() ([]Folder, error)
	FetchEnvelopes(folder string) ([]Message, error)
	FetchBody(folder string, uid imap.UID) (*Body, error)
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

	return FilterFolders(folders, s.acct.IMAP.HideFolders), nil
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
func defaultConnect(acct config.Account) (sessionClient, error) {
	password, err := acct.IMAPPassword()
	if err != nil {
		return nil, err
	}

	client, err := Connect(acct, password)
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

// NewManager builds a manager that connects accounts on first use.
func NewManager() *Manager {
	return &Manager{
		connect:  defaultConnect,
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
