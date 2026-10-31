package mail

import (
	"fmt"
	"time"

	"github.com/emersion/go-imap/v2"
)

// Message is one message as the message list needs it: envelope, flags and size,
// without the body. The body is fetched separately when a message is opened.
type Message struct {
	UID          imap.UID
	SeqNum       uint32
	Flags        []imap.Flag
	Size         int64
	InternalDate time.Time
	Envelope     imap.Envelope

	// MessageID and InReplyTo come from the envelope and are what threading
	// reads: a message attaches to the last In-Reply-To entry present in its
	// folder. Both are empty for a thread root.
	MessageID string
	InReplyTo []string
}

// SetFlag adds or removes a flag locally. It mirrors what the server confirmed,
// so the view can update without refetching the folder.
func (m *Message) SetFlag(flag imap.Flag, on bool) {
	for i, have := range m.Flags {
		if have != flag {
			continue
		}
		if on {
			return
		}
		m.Flags = append(m.Flags[:i], m.Flags[i+1:]...)

		return
	}

	if on {
		m.Flags = append(m.Flags, flag)
	}
}

// HasFlag reports whether the message carries a flag.
func (m Message) HasFlag(flag imap.Flag) bool {
	for _, have := range m.Flags {
		if have == flag {
			return true
		}
	}

	return false
}

// Seen reports whether the message has been read.
func (m Message) Seen() bool {
	return m.HasFlag(imap.FlagSeen)
}

// Flagged reports whether the message is starred.
func (m Message) Flagged() bool {
	return m.HasFlag(imap.FlagFlagged)
}

// Subject is the envelope subject, or a placeholder when it is empty.
func (m Message) Subject() string {
	if m.Envelope.Subject == "" {
		return "(no subject)"
	}

	return m.Envelope.Subject
}

// From is the first sender's display name, falling back to the address.
func (m Message) From() string {
	if len(m.Envelope.From) == 0 {
		return ""
	}

	addr := m.Envelope.From[0]
	if addr.Name != "" {
		return addr.Name
	}

	return addr.Addr()
}

// FromAddress is the first sender as a full address, which is what a reply's To header
// and the attribution line need. From() stays for display: a list column shows the name.
func (m Message) FromAddress() string {
	if len(m.Envelope.From) == 0 {
		return ""
	}

	return formatAddress(m.Envelope.From[0])
}

// ToAddresses and CcAddresses return full addresses, so reply-all can reuse them.
func (m Message) ToAddresses() []string {
	return formatAddresses(m.Envelope.To)
}

func (m Message) CcAddresses() []string {
	return formatAddresses(m.Envelope.Cc)
}

// formatAddress renders one envelope address as "Name <addr>" or a bare address.
func formatAddress(addr imap.Address) string {
	bare := addr.Addr()
	if addr.Name == "" {
		return bare
	}

	return fmt.Sprintf("%s <%s>", addr.Name, bare)
}

// formatAddresses renders every envelope address, skipping empty ones.
func formatAddresses(addresses []imap.Address) []string {
	out := make([]string, 0, len(addresses))

	for _, addr := range addresses {
		if rendered := formatAddress(addr); rendered != "" {
			out = append(out, rendered)
		}
	}

	return out
}

// Date is the envelope date, falling back to the server's internal date.
func (m Message) Date() time.Time {
	if !m.Envelope.Date.IsZero() {
		return m.Envelope.Date
	}

	return m.InternalDate
}
