package mail

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/renderorange/chute/config"
)

// blockingClient is a sessionClient whose folder listing blocks until the socket-only
// close runs, which is the shape of a body FETCH stalled on a literal.
type blockingClient struct {
	*fakeClient

	started  chan struct{}
	released chan struct{}
	once     sync.Once
}

func newBlockingClient() *blockingClient {
	return &blockingClient{
		fakeClient: &fakeClient{},
		started:    make(chan struct{}),
		released:   make(chan struct{}),
	}
}

func (b *blockingClient) Folders() ([]Folder, error) {
	b.once.Do(func() { close(b.started) })
	<-b.released

	return nil, errors.New("connection closed")
}

func (b *blockingClient) Abort() {
	b.aborted = true
	b.onceRelease()
}

func (b *blockingClient) onceRelease() {
	select {
	case <-b.released:
	default:
		close(b.released)
	}
}

// waitFor blocks until the operation in flight has started, so Close is measured while the
// command really is holding the session lock.
func (b *blockingClient) waitFor(t *testing.T) {
	t.Helper()

	select {
	case <-b.started:
	case <-time.After(5 * time.Second):
		t.Fatal("the blocking operation never started")
	}
}

// TestSessionCloseDoesNotWaitForAnInFlightCommand is #65 at the session: Close returns while
// a command holds the lock, the command then fails, and the session is usable no more.
func TestSessionCloseDoesNotWaitForAnInFlightCommand(t *testing.T) {
	client := newBlockingClient()
	session := newSession(config.Account{Name: "fastmail"}, func(config.Account) (sessionClient, error) {
		return client, nil
	})

	done := make(chan error, 1)
	go func() {
		_, err := session.Folders()
		done <- err
	}()

	client.waitFor(t)

	start := time.Now()
	if err := session.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Errorf("Close waited %v for an operation in flight", elapsed)
	}
	if !client.aborted {
		t.Error("Close did not close the socket; it logged out instead")
	}

	select {
	case err := <-done:
		if err == nil {
			t.Error("the interrupted operation reported success")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the interrupted operation never returned")
	}

	if _, err := session.Folders(); !errors.Is(err, errSessionClosed) {
		t.Errorf("after Close, Folders error = %v, want errSessionClosed", err)
	}
}

// TestManagerCloseDoesNotWaitForAStalledCommand is the same guarantee one level up: every
// account's session is closed individually and none of them blocks the quit.
func TestManagerCloseDoesNotWaitForAStalledCommand(t *testing.T) {
	client := newBlockingClient()
	manager := NewManager()
	manager.connect = func(config.Account) (sessionClient, error) { return client, nil }

	session := manager.For(config.Account{Name: "fastmail"})
	go func() { _, _ = session.Folders() }()
	client.waitFor(t)

	start := time.Now()
	if err := manager.Close(); err != nil {
		t.Fatalf("Manager.Close: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Errorf("Manager.Close waited %v for an operation in flight", elapsed)
	}
	if !client.aborted {
		t.Error("Manager.Close did not reach the socket")
	}
}

// stallingIMAPServer speaks just enough IMAP for a connection, a folder selection and a
// body FETCH, then announces a literal it never sends. It is the production shape of the
// hang: the client is blocked reading the body, not on a mutex.
type stallingIMAPServer struct {
	addr         string
	fetchStarted chan struct{}
	once         sync.Once
}

func newStallingIMAPServer(t *testing.T) *stallingIMAPServer {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	server := &stallingIMAPServer{addr: listener.Addr().String(), fetchStarted: make(chan struct{})}
	t.Cleanup(func() { listener.Close() })

	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}

			go server.serve(conn)
		}
	}()

	return server
}

// serve answers the handshake and then stalls on FETCH. Every other command is acknowledged,
// so a failure here comes from the stalled literal rather than a missing response.
func (s *stallingIMAPServer) serve(conn net.Conn) {
	defer conn.Close()

	if _, err := io.WriteString(conn, "* OK [CAPABILITY IMAP4rev1] ready\r\n"); err != nil {
		return
	}

	scanner := bufio.NewScanner(conn)

	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) == 0 {
			continue
		}

		tag := fields[0]
		command := strings.ToUpper(strings.Join(fields[1:], " "))

		switch {
		case strings.HasPrefix(command, "CAPABILITY"):
			fmt.Fprintf(conn, "* CAPABILITY IMAP4rev1\r\n%s OK done\r\n", tag)

		case strings.HasPrefix(command, "LOGIN"):
			fmt.Fprintf(conn, "%s OK logged in\r\n", tag)

		case strings.HasPrefix(command, "SELECT"):
			fmt.Fprintf(conn, "* 1 EXISTS\r\n* OK [UIDVALIDITY 1] valid\r\n%s OK [READ-WRITE] selected\r\n", tag)

		case strings.Contains(command, "FETCH"):
			// Announce nothing. The client is left waiting for a response that never comes,
			// which is the production hang: the command is blocked on the socket.
			s.once.Do(func() { close(s.fetchStarted) })

			_, _ = io.Copy(io.Discard, conn)

			return

		case strings.HasPrefix(command, "LOGOUT"):
			fmt.Fprintf(conn, "* BYE\r\n%s OK bye\r\n", tag)

			return

		default:
			fmt.Fprintf(conn, "%s OK done\r\n", tag)
		}
	}
}

// TestCloseReturnsWhileTheBodyFetchIsStalled is #65 against the real client: the literal is
// never sent, so the fetch is blocked on the network, and Close still returns at once.
func TestCloseReturnsWhileTheBodyFetchIsStalled(t *testing.T) {
	server := newStallingIMAPServer(t)
	acct := accountAt(t, server.addr)

	client, err := connectWith(dialInsecure, acct, "secret")
	if err != nil {
		t.Fatalf("connect: %v", err)
	}

	session := newSession(acct, func(config.Account) (sessionClient, error) { return client, nil })

	done := make(chan error, 1)
	go func() {
		_, err := session.FetchBody("INBOX", 1)
		done <- err
	}()

	select {
	case <-server.fetchStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("the client never sent FETCH")
	}

	start := time.Now()
	if err := session.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Errorf("Close waited %v for the stalled literal", elapsed)
	}

	select {
	case err := <-done:
		if err == nil {
			t.Error("the stalled fetch reported success")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the stalled fetch did not return after Close")
	}

	if _, err := session.Folders(); !errors.Is(err, errSessionClosed) {
		t.Errorf("after Close, Folders error = %v, want errSessionClosed", err)
	}
}

// TestCloseOnAnIdleSessionLogsOut keeps the polite path: an idle session still says LOGOUT
// before the socket goes away.
func TestCloseOnAnIdleSessionLogsOut(t *testing.T) {
	server := newTestServer(t, testCaps())
	session, _ := testSession(t, server)

	if _, err := session.Folders(); err != nil {
		t.Fatalf("Folders: %v", err)
	}

	if err := session.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	if log := server.log.String(); !strings.Contains(log, "LOGOUT") {
		t.Errorf("an idle session did not log out:\n%s", log)
	}
}
