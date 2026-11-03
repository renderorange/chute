package mail

import (
	"crypto/tls"
	"errors"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/emersion/go-imap/v2/imapclient"
)

// dialStartTLSInsecure is the production STARTTLS dialer with the in-process server's
// self-signed certificate accepted. connectWith has already set the server name.
func dialStartTLSInsecure(address string, options *imapclient.Options) (*imapclient.Client, error) {
	if options == nil {
		options = &imapclient.Options{}
	}
	if options.TLSConfig == nil {
		options.TLSConfig = &tls.Config{}
	}
	options.TLSConfig.InsecureSkipVerify = true

	return imapclient.DialStartTLS(address, options)
}

// probeFirstByte starts a socket that sends a plaintext IMAP greeting, records the
// first byte the client sends, and closes. A TLS ClientHello starts with 0x16; a
// plaintext command starts with a tag character. It is how the TLS mode can be observed
// without a certificate.
func probeFirstByte(t *testing.T) (string, <-chan byte) {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	first := make(chan byte, 1)

	go func() {
		conn, err := listener.Accept()
		if err != nil {
			close(first)
			return
		}
		defer conn.Close()

		if _, err := conn.Write([]byte("* OK probe ready\r\n")); err != nil {
			close(first)
			return
		}
		_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))

		one := make([]byte, 1)
		if _, err := io.ReadFull(conn, one); err == nil {
			first <- one[0]
		}
		close(first)
	}()

	t.Cleanup(func() { listener.Close() })

	return listener.Addr().String(), first
}

// TestConnectHonoursUseTLS is #60: `use_tls = false` was parsed, defaulted and then
// ignored, because Connect always dialed implicit TLS. A plaintext first byte proves the
// mode; the connection is expected to fail, since the probe is not a real server.
func TestConnectHonoursUseTLS(t *testing.T) {
	yes, no := true, false

	for _, tc := range []struct {
		name    string
		useTLS  *bool
		wantTLS bool
	}{
		{"unset dials TLS", nil, true},
		{"true dials TLS", &yes, true},
		{"false starts plaintext", &no, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			address, first := probeFirstByte(t)

			acct := accountAt(t, address)
			acct.IMAP.UseTLS = tc.useTLS

			// The probe is not an IMAP server, so the attempt must fail.
			if _, err := Connect(acct, "secret"); err == nil {
				t.Fatal("Connect succeeded against a one-byte probe")
			}

			b, ok := <-first
			if !ok {
				t.Fatal("the client sent no bytes")
			}

			if gotTLS := b == 0x16; gotTLS != tc.wantTLS {
				t.Errorf("first byte 0x%02x (TLS=%v), want TLS=%v", b, gotTLS, tc.wantTLS)
			}
		})
	}
}

// TestStartTLSDialerConnectsAndLogsIn proves the dialer the false branch selects works
// against a real STARTTLS server, including the post-upgrade LOGIN.
func TestStartTLSDialerConnectsAndLogsIn(t *testing.T) {
	server := newStartTLSServer(t, testCaps())

	client, err := connectWith(dialStartTLSInsecure, server.account(), "secret")
	if err != nil {
		t.Fatalf("connect with STARTTLS: %v", err)
	}
	defer client.Close()

	if !strings.Contains(server.log.String(), "STARTTLS") {
		t.Errorf("the server saw no STARTTLS command:\n%s", server.log.String())
	}
	if _, err := client.Folders(); err != nil {
		t.Fatalf("Folders over STARTTLS: %v", err)
	}
}

// TestStartTLSRefusesAServerThatCannotUpgrade pins the security ruling: use_tls = false
// means STARTTLS, not plaintext credentials. A server with no STARTTLS must be refused
// before LOGIN.
func TestStartTLSRefusesAServerThatCannotUpgrade(t *testing.T) {
	server := newTestServer(t, testCaps())

	_, err := connectWith(dialStartTLSInsecure, server.account(), "secret")
	if err == nil {
		t.Fatal("STARTTLS succeeded against a server with no TLSConfig")
	}
	if !errors.Is(err, ErrConnect) {
		t.Errorf("error = %v, want ErrConnect", err)
	}
	if strings.Contains(server.log.String(), "LOGIN") {
		t.Errorf("the client sent LOGIN in the clear:\n%s", server.log.String())
	}
}
