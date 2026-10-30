package mail

import (
	"errors"
	"testing"

	"github.com/emersion/go-imap/v2"
	"github.com/renderorange/chute/config"
)

// fakeClient stands in for a connected IMAP client in session tests.
type fakeClient struct {
	folders  []Folder
	messages []Message
	body     *Body
	err      error

	closed   bool
	folder   string
	uid      imap.UID
	requests int
}

func (f *fakeClient) Folders() ([]Folder, error) {
	f.requests++
	if f.err != nil {
		return nil, f.err
	}

	return f.folders, nil
}

func (f *fakeClient) FetchEnvelopes(folder string) ([]Message, error) {
	f.requests++
	f.folder = folder
	if f.err != nil {
		return nil, f.err
	}

	return f.messages, nil
}

func (f *fakeClient) FetchBody(folder string, uid imap.UID) (*Body, error) {
	f.requests++
	f.folder = folder
	f.uid = uid
	if f.err != nil {
		return nil, f.err
	}

	return f.body, nil
}

func (f *fakeClient) Close() error {
	f.closed = true

	return nil
}

func TestSessionReusesConnection(t *testing.T) {
	connects := 0
	connect := func(config.Account) (sessionClient, error) {
		connects++
		return &fakeClient{folders: []Folder{{Name: "INBOX"}}}, nil
	}
	session := newSession(config.Account{Name: "fastmail"}, connect)

	for i := 0; i < 3; i++ {
		if _, err := session.Folders(); err != nil {
			t.Fatalf("Folders call %d: %v", i, err)
		}
	}

	if connects != 1 {
		t.Errorf("connect calls = %d, want 1", connects)
	}
	if err := session.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

func TestSessionReconnectsAfterFailure(t *testing.T) {
	connects := 0
	var first *fakeClient
	connect := func(config.Account) (sessionClient, error) {
		connects++
		client := &fakeClient{}
		if connects == 1 {
			client.err = errors.New("connection reset")
			first = client
		}

		return client, nil
	}
	session := newSession(config.Account{Name: "fastmail"}, connect)

	if _, err := session.Folders(); err == nil {
		t.Fatal("first Folders returned no error")
	}
	if first == nil || !first.closed {
		t.Error("the failed client was not closed")
	}

	if _, err := session.Folders(); err != nil {
		t.Fatalf("second Folders: %v", err)
	}
	if connects != 2 {
		t.Errorf("connect calls = %d, want 2", connects)
	}
	if err := session.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

func TestSessionCloseDisconnects(t *testing.T) {
	client := &fakeClient{}
	session := newSession(config.Account{Name: "fastmail"}, func(config.Account) (sessionClient, error) {
		return client, nil
	})

	if _, err := session.Folders(); err != nil {
		t.Fatalf("Folders: %v", err)
	}
	if err := session.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if !client.closed {
		t.Error("Close did not close the connected client")
	}
	if _, err := session.Folders(); !errors.Is(err, errSessionClosed) {
		t.Errorf("Folders after Close = %v, want errSessionClosed", err)
	}
}

func TestSessionPropagatesOperations(t *testing.T) {
	client := &fakeClient{
		messages: []Message{{UID: 7}},
		body:     &Body{Text: "hello"},
	}
	session := newSession(config.Account{Name: "fastmail"}, func(config.Account) (sessionClient, error) {
		return client, nil
	})
	defer session.Close()

	messages, err := session.FetchEnvelopes("INBOX")
	if err != nil {
		t.Fatalf("FetchEnvelopes: %v", err)
	}
	if len(messages) != 1 || messages[0].UID != 7 {
		t.Errorf("messages = %+v, want one with UID 7", messages)
	}
	if client.folder != "INBOX" {
		t.Errorf("folder = %q, want INBOX", client.folder)
	}

	body, err := session.FetchBody("INBOX", 7)
	if err != nil {
		t.Fatalf("FetchBody: %v", err)
	}
	if got, want := body.Text, "hello"; got != want {
		t.Errorf("body text = %q, want %q", got, want)
	}
	if client.uid != 7 {
		t.Errorf("uid = %d, want 7", client.uid)
	}
}

func TestManagerReusesSessionPerAccount(t *testing.T) {
	manager := NewManager()
	defer manager.Close()

	first := manager.For(config.Account{Name: "fastmail"})
	again := manager.For(config.Account{Name: "fastmail"})
	other := manager.For(config.Account{Name: "work"})

	if first != again {
		t.Error("the same account got a second session")
	}
	if first == other {
		t.Error("different accounts share a session")
	}
}

func TestManagerCloseClosesSessions(t *testing.T) {
	client := &fakeClient{}
	manager := &Manager{
		connect:  func(config.Account) (sessionClient, error) { return client, nil },
		sessions: make(map[string]*Session),
	}

	session := manager.For(config.Account{Name: "fastmail"})
	if _, err := session.Folders(); err != nil {
		t.Fatalf("Folders: %v", err)
	}
	if err := manager.Close(); err != nil {
		t.Fatalf("Manager.Close: %v", err)
	}
	if !client.closed {
		t.Error("Manager.Close did not close the account connection")
	}
}
