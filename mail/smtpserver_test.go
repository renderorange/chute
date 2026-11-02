package mail

import (
	"bufio"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"math/big"
	"net"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/renderorange/chute/config"
	"time"
)

// scriptedServer is a minimal SMTP server that records the session. It exists so the
// send path is exercised against a real socket and a real SMTP conversation, including
// AUTH, which is what Fastmail uses.
type scriptedServer struct {
	addr     string
	implicit bool

	mu       sync.Mutex
	commands []string
	data     string
	authLine string

	// startTLS advertises STARTTLS and then breaks the handshake, so a test can prove
	// the client attempts the upgrade instead of sending mail in the clear.
	startTLS bool

	done chan struct{}
}

// commandsCopy returns the recorded client commands.
func (s *scriptedServer) commandsCopy() []string {
	s.mu.Lock()
	defer s.mu.Unlock()

	return append([]string(nil), s.commands...)
}

// dataCopy returns the DATA payload the server received.
func (s *scriptedServer) dataCopy() string {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.data
}

// authCopy returns the AUTH argument line the server received.
func (s *scriptedServer) authCopy() string {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.authLine
}

// served returns a channel closed when the server has finished the conversation, so a
// test can wait for it instead of racing it.
func (s *scriptedServer) served() <-chan struct{} {
	return s.done
}

// selfSignedTLSConfig builds a certificate for 127.0.0.1 so the implicit-TLS path can
// be tested without a real CA.
func selfSignedTLSConfig(t *testing.T) *tls.Config {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generating a key: %v", err)
	}

	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "127.0.0.1"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}

	der, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("creating a certificate: %v", err)
	}

	return &tls.Config{
		Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}},
	}
}

// newScriptedServer starts a server on loopback. With implicit TLS the listener speaks
// TLS from the first byte, which is port 465; without it the server speaks plain SMTP
// and never offers STARTTLS, which is the simple case.
func newScriptedServer(t *testing.T, implicit bool, authFail bool, startTLS bool) *scriptedServer {
	t.Helper()

	s := &scriptedServer{implicit: implicit, startTLS: startTLS}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	if implicit {
		listener = tls.NewListener(listener, selfSignedTLSConfig(t))
	}

	s.addr = listener.Addr().String()

	done := make(chan struct{})
	s.done = done

	go func() {
		defer close(done)
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()

		s.serve(conn, authFail)
	}()

	t.Cleanup(func() { listener.Close() })

	return s
}

// serve runs one SMTP conversation.
func (s *scriptedServer) serve(conn net.Conn, authFail bool) {
	reader := bufio.NewReader(conn)
	writer := bufio.NewWriter(conn)

	reply := func(line string) {
		// A failing test connection ends the conversation; the assertions that
		// matter are on the commands received, so the write error is not reported.
		_, _ = writer.WriteString(line + "\r\n")
		_ = writer.Flush()
	}

	reply("220 test.invalid ESMTP ready")

	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return
		}
		line = strings.TrimRight(line, "\r\n")

		s.mu.Lock()
		s.commands = append(s.commands, line)
		s.mu.Unlock()

		upper := strings.ToUpper(line)

		switch {
		case strings.HasPrefix(upper, "EHLO"), strings.HasPrefix(upper, "HELO"):
			reply("250-test.invalid")
			reply("250-AUTH PLAIN")
			if s.startTLS {
				reply("250-STARTTLS")
			}
			reply("250 OK")
		case upper == "STARTTLS":
			// Accept the command, then drop the connection: the client must treat the
			// failed upgrade as fatal rather than fall back to plaintext.
			reply("220 2.0.0 ready to start TLS")
			conn.Close()
			return
		case strings.HasPrefix(upper, "AUTH PLAIN"):
			s.mu.Lock()
			s.authLine = strings.TrimSpace(line[len("AUTH PLAIN"):])
			s.mu.Unlock()
			if authFail {
				reply("535 5.7.8 authentication failed")
				continue
			}
			reply("235 2.7.0 accepted")
		case strings.HasPrefix(upper, "MAIL FROM"), strings.HasPrefix(upper, "RCPT TO"):
			reply("250 OK")
		case upper == "DATA":
			reply("354 end with . on a line alone")

			var body strings.Builder
			for {
				dataLine, err := reader.ReadString('\n')
				if err != nil {
					return
				}
				if dataLine == ".\r\n" || dataLine == ".\n" {
					break
				}
				body.WriteString(dataLine)
			}

			s.mu.Lock()
			s.data = body.String()
			s.mu.Unlock()

			reply("250 2.0.0 queued")
		case upper == "QUIT":
			reply("221 2.0.0 bye")
			return
		default:
			reply("250 OK")
		}
	}
}

// dialScripted dials the scripted server. When the server speaks implicit TLS the
// production config is cloned with verification disabled, because the certificate is
// self-signed; that is the one thing a test may relax.
func dialScripted(addr string, implicitTLS bool, cfg *tls.Config) (net.Conn, error) {
	dialer := &net.Dialer{Timeout: 2 * time.Second}

	if !implicitTLS {
		return dialer.Dial("tcp", addr)
	}

	clone := cfg.Clone()
	clone.InsecureSkipVerify = true

	return tls.DialWithDialer(dialer, "tcp", addr, clone)
}

// testAccount builds an SMTP account pointing at the scripted server.
func testAccount(t *testing.T, addr string, implicit bool) config.Account {
	t.Helper()

	host, portText, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatalf("splitting %q: %v", addr, err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatalf("port %q: %v", portText, err)
	}

	useTLS := implicit

	return config.Account{
		Name: "memory",
		SMTP: config.SMTP{
			Host:     host,
			Port:     port,
			Username: "tester",
			UseTLS:   &useTLS,
		},
	}
}

// TestSendMessageAgainstScriptedServer is the happy path: AUTH happens, both
// recipients reach RCPT, and the DATA payload is exactly the built message.
func TestSendMessageAgainstScriptedServer(t *testing.T) {
	server := newScriptedServer(t, true, false, false)

	raw, err := BuildMessage(Compose{
		From:    "Blaine <blaine@example.org>",
		To:      []string{"one@example.com"},
		Cc:      []string{"two@example.com"},
		Subject: "sent by chute",
		Text:    "hello",
	})
	if err != nil {
		t.Fatalf("BuildMessage: %v", err)
	}

	err = sendMessage(testAccount(t, server.addr, true), "smtp-secret", raw, dialScripted, 5*time.Second)
	if err != nil {
		t.Fatalf("sendMessage: %v", err)
	}

	<-server.served()

	commands := server.commandsCopy()
	joined := strings.Join(commands, "\n")

	for _, want := range []string{"EHLO", "AUTH PLAIN", "MAIL FROM:<blaine@example.org>", "RCPT TO:<one@example.com>", "RCPT TO:<two@example.com>", "DATA", "QUIT"} {
		if !strings.Contains(joined, want) {
			t.Errorf("the session does not contain %q:\n%s", want, joined)
		}
	}

	if got := server.dataCopy(); normalizeCRLF(got) != normalizeCRLF(string(raw)) {
		t.Errorf("DATA payload differs from the built message:\ngot  %q\nwant %q", got, raw)
	}

	decoded, err := base64.StdEncoding.DecodeString(server.authCopy())
	if err != nil {
		t.Fatalf("AUTH argument is not base64: %v", err)
	}
	if !strings.Contains(string(decoded), "smtp-secret") {
		t.Errorf("AUTH payload does not carry the password: %q", decoded)
	}
}

// TestSendMessagePlaintextPath covers use_tls = false against a server that does not
// offer STARTTLS: the message still has to go.
func TestSendMessagePlaintextPath(t *testing.T) {
	server := newScriptedServer(t, false, false, false)

	raw, err := BuildMessage(Compose{
		From:    "blaine@example.org",
		To:      []string{"one@example.com"},
		Subject: "plain",
		Text:    "hello",
	})
	if err != nil {
		t.Fatalf("BuildMessage: %v", err)
	}

	if err := sendMessage(testAccount(t, server.addr, false), "smtp-secret", raw, dialScripted, 5*time.Second); err != nil {
		t.Fatalf("sendMessage: %v", err)
	}

	<-server.served()

	if !strings.Contains(server.dataCopy(), "Subject: plain") {
		t.Errorf("the server did not receive the message:\n%s", server.dataCopy())
	}
}

// TestSendMessageReportsAuthFailure proves a rejected login names the stage instead of
// reporting a generic send failure.
func TestSendMessageReportsAuthFailure(t *testing.T) {
	server := newScriptedServer(t, true, true, false)

	raw, err := BuildMessage(Compose{From: "blaine@example.org", To: []string{"one@example.com"}, Subject: "x"})
	if err != nil {
		t.Fatalf("BuildMessage: %v", err)
	}

	err = sendMessage(testAccount(t, server.addr, true), "wrong", raw, dialScripted, 5*time.Second)
	if err == nil {
		t.Fatal("sendMessage succeeded against a server that rejected the login")
	}
	if !strings.Contains(err.Error(), "auth") {
		t.Errorf("error %q does not name the auth stage", err)
	}
}

// TestSendMessageReportsConnectFailure proves nothing listening is reported as a
// connect failure.
func TestSendMessageReportsConnectFailure(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := listener.Addr().String()
	listener.Close()

	raw, err := BuildMessage(Compose{From: "blaine@example.org", To: []string{"one@example.com"}, Subject: "x"})
	if err != nil {
		t.Fatalf("BuildMessage: %v", err)
	}

	err = sendMessage(testAccount(t, addr, false), "secret", raw, dialScripted, time.Second)
	if err == nil {
		t.Fatal("sendMessage succeeded with nothing listening")
	}
	if !strings.Contains(err.Error(), "connect") {
		t.Errorf("error %q does not name the connect stage", err)
	}
}

// TestSendMessageHonoursTheDeadline proves a silent server cannot hang the program.
func TestSendMessageHonoursTheDeadline(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer listener.Close()

	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		time.Sleep(2 * time.Second)
	}()

	raw, err := BuildMessage(Compose{From: "blaine@example.org", To: []string{"one@example.com"}, Subject: "x"})
	if err != nil {
		t.Fatalf("BuildMessage: %v", err)
	}

	start := time.Now()
	err = sendMessage(testAccount(t, listener.Addr().String(), false), "secret", raw, dialScripted, 150*time.Millisecond)
	if err == nil {
		t.Fatal("sendMessage returned no error from a silent server")
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("sendMessage took %v with a 150ms deadline", elapsed)
	}
}

// normalizeCRLF makes the payload comparison independent of the SMTP dot-stuffing
// terminator.
func normalizeCRLF(s string) string {
	return strings.TrimSuffix(strings.ReplaceAll(s, "\r\n", "\n"), "\n")
}

// TestSendMessageAttemptsStartTLS proves a server that advertises STARTTLS is not
// allowed to receive the message in the clear: the client tries the upgrade, and a
// failed upgrade is an error rather than a downgrade.
func TestSendMessageAttemptsStartTLS(t *testing.T) {
	server := newScriptedServer(t, false, false, true)

	raw, err := BuildMessage(Compose{From: "blaine@example.org", To: []string{"one@example.com"}, Subject: "x"})
	if err != nil {
		t.Fatalf("BuildMessage: %v", err)
	}

	err = sendMessage(testAccount(t, server.addr, false), "smtp-secret", raw, dialScripted, 5*time.Second)
	if err == nil {
		t.Fatal("sendMessage succeeded after a failed STARTTLS upgrade")
	}
	if !strings.Contains(err.Error(), "starttls") {
		t.Errorf("error %q does not name the starttls stage", err)
	}

	<-server.served()

	if got := server.dataCopy(); got != "" {
		t.Errorf("the message was sent in the clear: %q", got)
	}
}
