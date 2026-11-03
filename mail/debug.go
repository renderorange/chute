package mail

import (
	"bytes"
	"io"
	"strings"
	"sync"

	"github.com/emersion/go-imap/v2/imapclient"
)

// redacted marks where a credential was masked in the protocol log.
const redacted = "[redacted]"

// Option configures how a connection is opened.
type Option func(*connectOptions)

// connectOptions is the set of things an Option can change.
type connectOptions struct {
	debugWriter io.Writer
	dial        dialFunc
}

// WithDebugWriter appends the IMAP protocol stream to w. The account credential is
// masked before it reaches w: login is the IMAP LOGIN command, so the password
// travels in that same stream.
func WithDebugWriter(w io.Writer) Option {
	return func(o *connectOptions) {
		o.debugWriter = w
	}
}

// withDial overrides the dialer. It is the seam the suite uses to reach the
// plaintext in-process server; production always dials TLS.
func withDial(dial dialFunc) Option {
	return func(o *connectOptions) {
		o.dial = dial
	}
}

// WithPlaintextDial connects without TLS, for a loopback server in a test.
//
// Connect dials TLS and nothing else, because a mail credential over a cleartext
// socket is a defect in production. The in-process server the suite runs against is
// plaintext, so a test that drives the whole program — config, manager, client,
// model — needs this seam before it can exist. It is not a configuration option: no
// config file can reach it, so no user can turn TLS off by accident.
func WithPlaintextDial() Option {
	return withDial(func(address string, options *imapclient.Options) (*imapclient.Client, error) {
		return imapclient.DialInsecure(address, options)
	})
}

// newConnectOptions applies opts in order.
func newConnectOptions(opts []Option) connectOptions {
	var o connectOptions

	for _, opt := range opts {
		if opt != nil {
			opt(&o)
		}
	}

	return o
}

// redactWriter masks a credential in an IMAP protocol stream.
//
// The password travels as a LOGIN argument, either as an RFC 3501 quoted string
// (with `"` and `\` escaped) or as a literal. Both the escaped and the raw form are
// replaced, escaped first because it is the longer sequence.
//
// It masks only sequences contained in one Write. The tests that drive the real
// encoder against the in-process server are the evidence that imapclient does not
// split the credential across writes; this comment claims no more than they show.
//
// imapclient tees both directions into the same writer from its reader and writer
// goroutines, so the mutex is required.
type redactWriter struct {
	mu      sync.Mutex
	w       io.Writer
	secrets [][]byte
}

// newRedactWriter wraps w so password never reaches it. An empty password leaves
// the stream untouched, so an unresolved credential cannot mask unrelated text.
func newRedactWriter(w io.Writer, password string) io.Writer {
	if password == "" {
		return w
	}

	return &redactWriter{
		w:       w,
		secrets: [][]byte{[]byte(quoted(password)), []byte(password)},
	}
}

// Write masks the credential and passes the rest of the stream through.
func (r *redactWriter) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	masked := p
	for _, secret := range r.secrets {
		masked = bytes.ReplaceAll(masked, secret, []byte(redacted))
	}

	if _, err := r.w.Write(masked); err != nil {
		return 0, err
	}

	// Report the input length: the caller is a tee that only cares whether the
	// bytes made it through.
	return len(p), nil
}

// quoted renders s the way an IMAP quoted string does.
func quoted(s string) string {
	var b strings.Builder

	b.Grow(len(s) + 2)
	b.WriteByte('"')

	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '"', '\\':
			b.WriteByte('\\')
		}
		b.WriteByte(s[i])
	}

	b.WriteByte('"')

	return b.String()
}
