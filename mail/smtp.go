package mail

import (
	"bytes"
	"crypto/tls"
	"fmt"
	"net"
	"net/mail"
	"net/smtp"
	"strings"
	"time"

	"github.com/renderorange/chute/config"
)

// smtpTimeout bounds the whole SMTP conversation: dial, greeting, auth and DATA.
const smtpTimeout = 30 * time.Second

// smtpDialFunc opens the transport. It is a parameter of sendMessage so the suite can
// reach a loopback server with a self-signed certificate; production always uses
// dialSMTP.
type smtpDialFunc func(address string, implicitTLS bool, cfg *tls.Config) (net.Conn, error)

// SendMessage delivers msg through the account's SMTP server.
//
// The envelope is read from the message itself: From supplies MAIL FROM, and To plus
// Cc supply RCPT TO. That way the envelope cannot drift from the headers, because there
// is only one copy of each address.
func SendMessage(acct config.Account, password string, msg []byte) error {
	return sendMessage(acct, password, msg, dialSMTP, smtpTimeout)
}

// dialSMTP is the production dialer: implicit TLS when the account asks for it.
func dialSMTP(address string, implicitTLS bool, cfg *tls.Config) (net.Conn, error) {
	dialer := &net.Dialer{Timeout: smtpTimeout}
	if implicitTLS {
		return tls.DialWithDialer(dialer, "tcp", address, cfg)
	}

	return dialer.Dial("tcp", address)
}

// sendMessage is the whole send path, with the dialer and deadline injected.
func sendMessage(acct config.Account, password string, msg []byte, dial smtpDialFunc, timeout time.Duration) error {
	envelope, err := envelopeOf(msg)
	if err != nil {
		return fmt.Errorf("mail: account %q: %w", acct.Name, err)
	}

	implicitTLS := acct.SMTP.UseTLS == nil || *acct.SMTP.UseTLS
	tlsConfig := &tls.Config{ServerName: acct.SMTP.Host}

	conn, err := dial(acct.SMTPAddress(), implicitTLS, tlsConfig)
	if err != nil {
		return fmt.Errorf("%w: account %q: connect to %s: %w", ErrConnect, acct.Name, acct.SMTPAddress(), err)
	}
	defer conn.Close()

	if err := conn.SetDeadline(time.Now().Add(timeout)); err != nil {
		return fmt.Errorf("%w: account %q: connect to %s: %w", ErrConnect, acct.Name, acct.SMTPAddress(), err)
	}

	client, err := smtp.NewClient(conn, acct.SMTP.Host)
	if err != nil {
		return fmt.Errorf("%w: account %q: connect to %s: %w", ErrConnect, acct.Name, acct.SMTPAddress(), err)
	}
	defer client.Close()

	if !implicitTLS {
		if ok, _ := client.Extension("STARTTLS"); ok {
			if err := client.StartTLS(tlsConfig); err != nil {
				return fmt.Errorf("mail: account %q: starttls: %w", acct.Name, err)
			}
		}
	}

	if ok, _ := client.Extension("AUTH"); ok {
		auth := smtp.PlainAuth("", acct.SMTP.Username, password, acct.SMTP.Host)
		if err := client.Auth(auth); err != nil {
			return fmt.Errorf("%w: account %q: auth as %s: %w", ErrAuth, acct.Name, acct.SMTP.Username, err)
		}
	}

	if err := client.Mail(envelope.from); err != nil {
		return fmt.Errorf("mail: account %q: mail from: %w", acct.Name, err)
	}
	for _, rcpt := range envelope.rcpt {
		if err := client.Rcpt(rcpt); err != nil {
			return fmt.Errorf("mail: account %q: rcpt to %s: %w", acct.Name, rcpt, err)
		}
	}

	writer, err := client.Data()
	if err != nil {
		return fmt.Errorf("mail: account %q: data: %w", acct.Name, err)
	}
	if _, err := writer.Write(msg); err != nil {
		writer.Close()
		return fmt.Errorf("mail: account %q: data: %w", acct.Name, err)
	}
	if err := writer.Close(); err != nil {
		return fmt.Errorf("mail: account %q: data: %w", acct.Name, err)
	}

	if err := client.Quit(); err != nil {
		return fmt.Errorf("mail: account %q: quit: %w", acct.Name, err)
	}

	return nil
}

// envelope is the SMTP-level sender and recipients, taken from the message headers.
type envelope struct {
	from string
	rcpt []string
}

// envelopeOf reads From, To and Cc out of a built message.
func envelopeOf(msg []byte) (envelope, error) {
	parsed, err := mail.ReadMessage(bytes.NewReader(msg))
	if err != nil {
		return envelope{}, fmt.Errorf("reading the message for its envelope: %w", err)
	}

	from, err := mail.ParseAddress(parsed.Header.Get("From"))
	if err != nil {
		return envelope{}, fmt.Errorf("reading the From address: %w", err)
	}

	var rcpt []string

	for _, field := range []string{"To", "Cc"} {
		value := strings.TrimSpace(parsed.Header.Get(field))
		if value == "" {
			continue
		}

		addresses, err := mail.ParseAddressList(value)
		if err != nil {
			return envelope{}, fmt.Errorf("reading the %s addresses: %w", field, err)
		}
		for _, addr := range addresses {
			rcpt = append(rcpt, addr.Address)
		}
	}

	if len(rcpt) == 0 {
		return envelope{}, fmt.Errorf("reading the message: no To or Cc recipients")
	}

	return envelope{from: from.Address, rcpt: rcpt}, nil
}
