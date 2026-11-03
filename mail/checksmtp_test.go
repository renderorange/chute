package mail

import (
	"errors"
	"strings"
	"testing"
)

// TestCheckSMTPSendsNothing is #66: the preflight authenticates and quits, and never opens
// an envelope or a DATA phase. AUTH is the strongest proof of reachability that does not
// deliver mail.
func TestCheckSMTPSendsNothing(t *testing.T) {
	server := newScriptedServer(t, false, false, false)

	if err := CheckSMTP(testAccount(t, server.addr, false), "smtp-secret"); err != nil {
		t.Fatalf("CheckSMTP: %v", err)
	}

	<-server.served()

	joined := strings.Join(server.commandsCopy(), "\n")
	for _, want := range []string{"EHLO", "AUTH PLAIN", "QUIT"} {
		if !strings.Contains(joined, want) {
			t.Errorf("the check does not contain %q:\n%s", want, joined)
		}
	}
	for _, banned := range []string{"MAIL FROM", "RCPT TO", "DATA"} {
		if strings.Contains(joined, banned) {
			t.Errorf("the check sent %q:\n%s", banned, joined)
		}
	}
	if body := server.dataCopy(); body != "" {
		t.Errorf("the check transmitted a body: %q", body)
	}
}

// TestCheckSMTPReportsARejectedPassword keeps the check's failure typed, so a caller can
// tell a bad credential from a bad host.
func TestCheckSMTPReportsARejectedPassword(t *testing.T) {
	server := newScriptedServer(t, false, true, false)

	err := CheckSMTP(testAccount(t, server.addr, false), "wrong")
	if !errors.Is(err, ErrAuth) {
		t.Fatalf("error = %v, want ErrAuth", err)
	}
}

// TestCheckSMTPReportsAnUnreachableServer is the wrong-port case: the error names the
// address that was tried.
func TestCheckSMTPReportsAnUnreachableServer(t *testing.T) {
	address := closedAddress(t)

	err := CheckSMTP(testAccount(t, address, false), "secret")
	if !errors.Is(err, ErrConnect) {
		t.Fatalf("error = %v, want ErrConnect", err)
	}
	if !strings.Contains(err.Error(), address) {
		t.Errorf("error = %v, want it to name %s", err, address)
	}
}
