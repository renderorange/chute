package main

import (
	"bufio"
	"bytes"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/renderorange/chute/config"
)

// smtpStub is a plaintext SMTP server with just enough conversation for -check: a greeting,
// EHLO advertising AUTH, an AUTH answer, and QUIT. It records every command so a test can
// prove no envelope was opened.
type smtpStub struct {
	addr   string
	reject bool
	done   chan struct{}

	mu    sync.Mutex
	lines []string
}

func newSMTPStub(t *testing.T, rejectAuth bool) *smtpStub {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	stub := &smtpStub{addr: listener.Addr().String(), reject: rejectAuth, done: make(chan struct{})}
	t.Cleanup(func() { listener.Close() })

	go func() {
		defer close(stub.done)

		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()

		reader := bufio.NewReader(conn)
		reply := func(line string) { _, _ = fmt.Fprintf(conn, "%s\r\n", line) }

		reply("220 stub ready")

		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				return
			}

			line = strings.TrimRight(line, "\r\n")
			stub.record(line)

			upper := strings.ToUpper(line)
			switch {
			case strings.HasPrefix(upper, "EHLO"), strings.HasPrefix(upper, "HELO"):
				reply("250-stub")
				reply("250-AUTH PLAIN")
				reply("250 OK")

			case strings.HasPrefix(upper, "AUTH PLAIN"):
				if stub.reject {
					reply("535 5.7.8 authentication failed")

					continue
				}
				reply("235 2.7.0 accepted")

			case upper == "QUIT":
				reply("221 bye")

				return

			default:
				reply("250 OK")
			}
		}
	}()

	return stub
}

func (s *smtpStub) record(line string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.lines = append(s.lines, line)
}

// commands returns what the client said, so a test can look for a missing DATA phase.
func (s *smtpStub) commands() []string {
	s.mu.Lock()
	defer s.mu.Unlock()

	return append([]string(nil), s.lines...)
}

// wait blocks until the stub's conversation is over, so nothing is read while it runs.
func (s *smtpStub) wait(t *testing.T) {
	t.Helper()

	select {
	case <-s.done:
	case <-time.After(5 * time.Second):
		t.Fatal("the SMTP stub did not finish")
	}
}

func (s *smtpStub) port(t *testing.T) int {
	t.Helper()

	_, portText, err := net.SplitHostPort(s.addr)
	if err != nil {
		t.Fatalf("splitting %q: %v", s.addr, err)
	}

	var port int
	if _, err := fmt.Sscanf(portText, "%d", &port); err != nil {
		t.Fatalf("port %q: %v", portText, err)
	}

	return port
}

// closedPort returns a loopback port with nothing listening on it.
func closedPort(t *testing.T) int {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()

	return port
}

// checkConfig builds a config whose IMAP points at a closed port and whose SMTP points at
// the given one, both plaintext, so a test drives the two halves separately.
func checkConfig(imapPort, smtpPort int) *config.Config {
	no := false

	return &config.Config{
		Accounts: []config.Account{{
			Name: "test",
			IMAP: config.IMAP{Host: "127.0.0.1", Port: imapPort, Username: "imap-user", Password: "imap-secret"},
			SMTP: config.SMTP{
				Host:     "127.0.0.1",
				Port:     smtpPort,
				Username: "smtp-user",
				Password: "smtp-secret",
				UseTLS:   &no,
			},
		}},
	}
}

// TestCheckSMTMPassesWithoutSending is the healthy half: AUTH and QUIT, no MAIL FROM, no
// RCPT, no DATA.
func TestCheckSMTMPassesWithoutSending(t *testing.T) {
	stub := newSMTPStub(t, false)
	cfg := checkConfig(closedPort(t), stub.port(t))

	var stdout, stderr bytes.Buffer
	if !checkSMTP(&cfg.Accounts[0], &stdout, &stderr) {
		t.Fatalf("checkSMTP failed: %s", stderr.String())
	}
	stub.wait(t)

	for _, banned := range []string{"MAIL FROM", "RCPT TO", "DATA"} {
		for _, line := range stub.commands() {
			if strings.Contains(strings.ToUpper(line), banned) {
				t.Errorf("the check sent %q", banned)
			}
		}
	}

	out := stdout.String()
	if !strings.Contains(out, "smtp ") || !strings.Contains(out, "tls=starttls") {
		t.Errorf("stdout does not describe the SMTP check: %q", out)
	}
	if strings.Contains(out, "smtp-secret") {
		t.Errorf("stdout carries the credential: %q", out)
	}
}

// TestCheckSMTPReportsABadPort is the acceptance case: the error names the host:port and the
// account fails.
func TestCheckSMTPReportsABadPort(t *testing.T) {
	port := closedPort(t)
	cfg := checkConfig(closedPort(t), port)

	var stdout, stderr bytes.Buffer
	if checkSMTP(&cfg.Accounts[0], &stdout, &stderr) {
		t.Fatal("checkSMTP passed against a closed port")
	}

	want := fmt.Sprintf("127.0.0.1:%d", port)
	if !strings.Contains(stderr.String(), want) {
		t.Errorf("stderr = %q, want it to name %s", stderr.String(), want)
	}
}

// TestCheckSMTPReportsARejectedPassword keeps the short auth text, which is the one a user
// can act on.
func TestCheckSMTPReportsARejectedPassword(t *testing.T) {
	stub := newSMTPStub(t, true)
	cfg := checkConfig(closedPort(t), stub.port(t))

	var stdout, stderr bytes.Buffer
	if checkSMTP(&cfg.Accounts[0], &stdout, &stderr) {
		t.Fatal("checkSMTP passed with a rejected password")
	}
	stub.wait(t)

	if !strings.Contains(stderr.String(), "authentication rejected") {
		t.Errorf("stderr = %q, want the authentication text", stderr.String())
	}
}

// TestRunCheckFailsWhenEitherHalfFails proves the exit status covers SMTP: IMAP is dead here
// and the SMTP check still runs, still reports, and the overall result is false.
func TestRunCheckFailsWhenEitherHalfFails(t *testing.T) {
	stub := newSMTPStub(t, false)
	cfg := checkConfig(closedPort(t), stub.port(t))

	var stdout, stderr bytes.Buffer
	if runCheck(cfg, &stdout, &stderr) {
		t.Fatal("runCheck reported success with a dead IMAP port")
	}
	if !strings.Contains(stdout.String(), "smtp ") {
		t.Errorf("runCheck did not print the SMTP half: stdout %q", stdout.String())
	}
	stub.wait(t)
}

// TestRunCheckNeverPrintsTheCredentials covers both halves: the account, user and transport
// may be printed, the password may not.
func TestRunCheckNeverPrintsTheCredentials(t *testing.T) {
	stub := newSMTPStub(t, false)
	cfg := checkConfig(closedPort(t), stub.port(t))

	var stdout, stderr bytes.Buffer
	runCheck(cfg, &stdout, &stderr)
	stub.wait(t)

	combined := stdout.String() + stderr.String()
	for _, secret := range []string{"imap-secret", "smtp-secret"} {
		if strings.Contains(combined, secret) {
			t.Errorf("the check printed %q", secret)
		}
	}
}

// TestCheckSMTPNamesAnInheritedHost proves the fallback is visible: a config with no
// [accounts.smtp] host reports that the address came from [accounts.imap].
func TestCheckSMTPNamesAnInheritedHost(t *testing.T) {
	stub := newSMTPStub(t, false)

	body := fmt.Sprintf(`
[[accounts]]
name = "inherited"

[accounts.imap]
host = "127.0.0.1"
port = %d
username = "shared"
password = "imap-secret"
use_tls = false

[accounts.smtp]
port = %d
use_tls = false
`, closedPort(t), stub.port(t))

	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("writing the config: %v", err)
	}

	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !cfg.Accounts[0].SMTP.HostInherited() {
		t.Fatal("the loaded account does not record the inherited SMTP host")
	}

	var stdout, stderr bytes.Buffer
	if !checkSMTP(&cfg.Accounts[0], &stdout, &stderr) {
		t.Fatalf("checkSMTP failed: %s", stderr.String())
	}
	stub.wait(t)

	out := stdout.String()
	if !strings.Contains(out, "host=inherited") {
		t.Errorf("stdout does not report the inherited host: %q", out)
	}
	if !strings.Contains(out, "as shared") {
		t.Errorf("stdout does not report the inherited username: %q", out)
	}
}
