package mail

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/mail"
	"strings"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-message"
)

// Compose is an outgoing message: what the composer holds and what the mail layer
// builds into RFC 5322 bytes.
type Compose struct {
	From      string
	Subject   string
	Text      string
	MessageID string

	To []string
	Cc []string

	InReplyTo  string
	References []string

	// HadAttachments records that the source message carried attachments that this
	// composer cannot re-attach. It is set by parseCompose and ignored by BuildMessage.
	HadAttachments bool
}

// BuildMessage turns a Compose into RFC 5322 bytes with CRLF line endings, ready for
// APPEND and for SMTP DATA. MessageID is generated when the caller has not set one;
// the composer sets it once so the draft and the sent copy share an id.
func BuildMessage(c Compose) ([]byte, error) {
	if err := checkHeaderField("From", c.From); err != nil {
		return nil, err
	}
	if err := checkHeaderField("Subject", c.Subject); err != nil {
		return nil, err
	}
	for _, addr := range c.To {
		if err := checkHeaderField("To", addr); err != nil {
			return nil, err
		}
	}
	for _, addr := range c.Cc {
		if err := checkHeaderField("Cc", addr); err != nil {
			return nil, err
		}
	}

	if strings.TrimSpace(c.From) == "" {
		return nil, errors.New("mail: composing a message with no From address")
	}

	id := bareID(c.MessageID)
	if id == "" {
		id = generateMessageID(c.From)
	}

	header := message.Header{}
	header.Set("From", c.From)
	header.Set("To", strings.Join(c.To, ", "))
	if len(c.Cc) > 0 {
		header.Set("Cc", strings.Join(c.Cc, ", "))
	}
	header.Set("Subject", c.Subject)
	header.Set("Date", time.Now().Format(time.RFC1123Z))
	header.Set("Message-ID", headerID(id))
	header.Set("Content-Type", "text/plain; charset=utf-8")
	header.Set("Content-Transfer-Encoding", "8bit")

	if inReplyTo := bareID(c.InReplyTo); inReplyTo != "" {
		header.Set("In-Reply-To", headerID(inReplyTo))
	}
	if references := headerIDs(c.References); len(references) > 0 {
		header.Set("References", strings.Join(references, " "))
	}

	var buf bytes.Buffer

	writer, err := message.CreateWriter(&buf, header)
	if err != nil {
		return nil, fmt.Errorf("mail: building the message: %w", err)
	}
	if _, err := writer.Write([]byte(toCRLF(c.Text))); err != nil {
		return nil, fmt.Errorf("mail: writing the message body: %w", err)
	}
	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("mail: closing the message: %w", err)
	}

	return buf.Bytes(), nil
}

// checkHeaderField rejects a newline in a header value. Folding it silently would let
// a crafted recipient string add headers, which is the whole game for header injection.
func checkHeaderField(name, value string) error {
	if strings.ContainsAny(value, "\r\n") {
		return fmt.Errorf("mail: the %s header contains a newline", name)
	}

	return nil
}

// toCRLF normalizes line endings to CRLF, which SMTP requires.
func toCRLF(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")

	return strings.ReplaceAll(s, "\n", "\r\n")
}

// FetchCompose reads one message as an outgoing message, so a saved draft reopens in
// the composer with its fields intact.
func (c *Client) FetchCompose(folder string, uid imap.UID) (*Compose, error) {
	raw, err := c.fetchRaw(folder, uid)
	if err != nil {
		return nil, err
	}

	return parseCompose(raw)
}

// parseCompose maps a raw message onto the fields the composer edits.
func parseCompose(raw []byte) (*Compose, error) {
	entity, err := message.Read(bytes.NewReader(raw))
	if err != nil && !message.IsUnknownCharset(err) && !message.IsUnknownEncoding(err) {
		return nil, fmt.Errorf("mail: reading the message: %w", err)
	}
	if entity == nil {
		return nil, errors.New("mail: reading the message: no content")
	}

	body, err := parseBody(raw)
	if err != nil {
		return nil, err
	}

	// Header.Text decodes RFC 2047 and returns the raw value with an
	// IsUnknownCharset error, which is still usable text.
	from, _ := entity.Header.Text("From")
	subject, _ := entity.Header.Text("Subject")

	return &Compose{
		From:           from,
		Subject:        subject,
		Text:           body.Text,
		MessageID:      bareID(entity.Header.Get("Message-ID")),
		To:             splitAddresses(entity.Header.Get("To")),
		Cc:             splitAddresses(entity.Header.Get("Cc")),
		InReplyTo:      bareID(firstToken(entity.Header.Get("In-Reply-To"))),
		References:     bareIDs(strings.Fields(entity.Header.Get("References"))),
		HadAttachments: len(body.Attachments) > 0,
	}, nil
}

// splitAddresses turns a header value into one string per address, keeping display
// names. A value the parser rejects is kept whole rather than dropped: losing a
// recipient silently is worse than showing a messy field to edit.
func splitAddresses(value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}

	list, err := mail.ParseAddressList(value)
	if err != nil {
		return []string{value}
	}

	out := make([]string, 0, len(list))
	for _, addr := range list {
		out = append(out, addr.String())
	}

	return out
}

// firstToken returns the first whitespace-separated token, which is how In-Reply-To
// carries a single message id.
func firstToken(value string) string {
	fields := strings.Fields(value)
	if len(fields) == 0 {
		return ""
	}

	return fields[0]
}

// NewMessageID returns a fresh bare Message-ID in the given From address's domain. The
// composer calls it once per message so the draft and the sent copy match; BuildMessage
// adds the angle brackets when it writes the header.
func NewMessageID(from string) string {
	return generateMessageID(from)
}

// generateMessageID builds a unique bare id in the From address's domain, falling back
// to chute.local when the From value carries no domain.
func generateMessageID(from string) string {
	domain := "chute.local"

	if addr, err := mail.ParseAddress(from); err == nil {
		if at := strings.LastIndex(addr.Address, "@"); at >= 0 && at+1 < len(addr.Address) {
			domain = addr.Address[at+1:]
		}
	}

	var random [8]byte
	if _, err := rand.Read(random[:]); err != nil {
		return fmt.Sprintf("%d@%s", time.Now().UnixNano(), domain)
	}

	return fmt.Sprintf("%d.%s@%s", time.Now().UnixNano(), hex.EncodeToString(random[:]), domain)
}
