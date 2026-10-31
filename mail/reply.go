package mail

import (
	"fmt"
	"net/mail"
	"strings"
	"time"
)

// ReplyOptions carries what the caller knows that the original message does not.
type ReplyOptions struct {
	// From is the account's own From value, placed on the new message.
	From string
	// Self is the account's bare address, dropped from reply-all recipients.
	Self string
	// Body is the reply text.
	Body string
	// Attribution introduces the quoted original. Attribution builds it.
	Attribution string
	// All includes the original's To and Cc, minus Self.
	All bool
	// Bottom places the reply after the quote instead of before it.
	Bottom bool
}

// Attribution is the line that introduces a quoted original.
func Attribution(from string, when time.Time) string {
	who := strings.TrimSpace(from)
	if who == "" {
		who = "unknown sender"
	}

	return fmt.Sprintf("On %s, %s wrote:", when.Format("2006-01-02 15:04"), who)
}

// ReplyCompose builds the reply to orig: subject prefixed once, the thread headers
// chained, the whole original quoted, and the reply area above or below it.
func ReplyCompose(orig Compose, opts ReplyOptions) Compose {
	quote := quoteText(orig.Text)

	var text string
	if opts.Bottom {
		text = opts.Attribution + "\n\n" + quote
		if opts.Body != "" {
			text += opts.Body + "\n"
		}
	} else {
		text = opts.Body + "\n" + opts.Attribution + "\n\n" + quote
	}

	reply := Compose{
		From:    opts.From,
		Subject: prefixedSubject(orig.Subject, "Re: "),
		Text:    text,
		To:      []string{orig.From},
	}

	if opts.All {
		reply.To = append(reply.To, withoutAddress(orig.To, opts.Self, orig.From)...)
		reply.Cc = withoutAddress(orig.Cc, opts.Self, orig.From)
	}

	if orig.MessageID != "" {
		reply.InReplyTo = orig.MessageID
		reply.References = append(append([]string(nil), orig.References...), orig.MessageID)
	}

	return reply
}

// ForwardCompose builds a forward: no quoting, the original below a separator, no
// threading headers, and no recipients.
func ForwardCompose(orig Compose, from, bodyText string) Compose {
	text := bodyText
	if text != "" && !strings.HasSuffix(text, "\n") {
		text += "\n"
	}
	text += "\n---------- Forwarded message ----------\n"
	text += orig.Text
	if !strings.HasSuffix(text, "\n") {
		text += "\n"
	}

	return Compose{
		From:    from,
		Subject: prefixedSubject(orig.Subject, "Fwd: "),
		Text:    text,
	}
}

// quoteText prefixes every line with "> ", including empty ones, which are a bare ">".
func quoteText(text string) string {
	text = strings.TrimSuffix(text, "\n")
	if text == "" {
		return ">\n"
	}

	lines := strings.Split(text, "\n")
	for i, line := range lines {
		if line == "" {
			lines[i] = ">"
			continue
		}
		lines[i] = "> " + line
	}

	return strings.Join(lines, "\n") + "\n"
}

// prefixedSubject adds prefix unless the subject already starts with it, case
// insensitively, so "Re: Re:" never appears.
func prefixedSubject(subject, prefix string) string {
	if strings.HasPrefix(strings.ToLower(strings.TrimLeft(subject, " ")), strings.ToLower(prefix)) {
		return subject
	}

	return prefix + subject
}

// withoutAddress removes every entry whose bare address matches self, keeping the
// original strings (and their display names) for everyone else.
func withoutAddress(addresses []string, self string, already ...string) []string {
	drop := map[string]bool{}
	for _, value := range append([]string{self}, already...) {
		if bare := bareAddress(value); bare != "" {
			drop[bare] = true
		}
	}

	out := make([]string, 0, len(addresses))
	for _, value := range addresses {
		if drop[bareAddress(value)] {
			continue
		}
		out = append(out, value)
	}

	return out
}

// bareAddress returns the lower-cased address from a value that may carry a display
// name. An unparsable value is compared as written, so a typo still matches itself.
func bareAddress(value string) string {
	addr, err := mail.ParseAddress(strings.TrimSpace(value))
	if err != nil {
		return strings.ToLower(strings.TrimSpace(value))
	}

	return strings.ToLower(addr.Address)
}
