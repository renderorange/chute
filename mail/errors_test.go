package mail

import (
	"errors"
	"net"
	"strconv"
	"strings"
	"testing"

	"github.com/emersion/go-imap/v2"
	"github.com/renderorange/chute/config"
)

// closedAddress returns a loopback address with nothing listening on it.
func closedAddress(t *testing.T) string {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	address := listener.Addr().String()
	listener.Close()

	return address
}

// accountAt builds an account for one bare loopback address.
func accountAt(t *testing.T, address string) config.Account {
	t.Helper()

	host, portText, err := net.SplitHostPort(address)
	if err != nil {
		t.Fatalf("split %q: %v", address, err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatalf("port %q: %v", portText, err)
	}

	return config.Account{
		Name: "fastmail",
		IMAP: config.IMAP{Host: host, Port: port, Username: "tester"},
	}
}

// TestConnectFailureIsTyped is the first whitelist entry: the TUI has to be able to say
// "cannot connect" without showing the dial chain.
func TestConnectFailureIsTyped(t *testing.T) {
	_, err := connectWith(dialInsecure, accountAt(t, closedAddress(t)), "secret")
	if err == nil {
		t.Fatal("connecting with nothing listening returned no error")
	}

	if !errors.Is(err, ErrConnect) {
		t.Errorf("error %v does not match ErrConnect", err)
	}
	if errors.Is(err, ErrAuth) {
		t.Error("a refused connection was reported as an auth failure")
	}
}

// TestLoginRejectionIsTypedAndHidesThePassword covers the auth phrase and the issue's
// credential rule: the displayed error must not contain the password.
func TestLoginRejectionIsTypedAndHidesThePassword(t *testing.T) {
	const password = "s3cret-pass"

	server := newTestServer(t, testCaps())

	_, err := connectWith(dialInsecure, server.account(), password)
	if err == nil {
		t.Fatal("a wrong password logged in")
	}

	if !errors.Is(err, ErrAuth) {
		t.Errorf("error %v does not match ErrAuth", err)
	}
	if errors.Is(err, ErrConnect) {
		t.Error("a rejected login was reported as a connect failure")
	}
	if strings.Contains(err.Error(), password) {
		t.Errorf("the credential reached the error text: %v", err)
	}
}

// TestMoveWithoutCapabilitiesIsTyped is the capability phrase, shared by delete,
// archive and junk because they share MoveToFolder.
func TestMoveWithoutCapabilitiesIsTyped(t *testing.T) {
	server := newTestServer(t, imap.CapSet{imap.CapIMAP4rev1: {}})
	server.createMailbox(t, "Archive")
	client := server.connect(t)

	err := client.MoveToFolder("INBOX", "Archive", []imap.UID{1})
	if err == nil {
		t.Fatal("MoveToFolder succeeded without MOVE or UIDPLUS")
	}
	if !errors.Is(err, ErrNoMoveSupport) {
		t.Errorf("error %v does not match ErrNoMoveSupport", err)
	}
}

// TestDeleteWithoutTrashIsTyped is the third role refusal the whitelist names. The
// wrapped text keeps the actionable hint for logs.
func TestDeleteWithoutTrashIsTyped(t *testing.T) {
	client := &fakeClient{folders: []Folder{{Name: "INBOX", Delimiter: '/'}}}
	session := newMoveSession(t, config.Account{Name: "fastmail"}, client)

	if _, err := session.Folders(); err != nil {
		t.Fatalf("Folders: %v", err)
	}

	err := session.Delete("INBOX", []imap.UID{1})
	if err == nil {
		t.Fatal("Delete succeeded without a trash folder")
	}
	if !errors.Is(err, ErrNoRoleFolder) {
		t.Errorf("error %v does not match ErrNoRoleFolder", err)
	}
	if !strings.Contains(err.Error(), "trash") {
		t.Errorf("error %q lost the trash hint", err)
	}
}
