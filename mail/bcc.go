package mail

import (
	"bytes"
	"strings"
)

// StripBcc returns the message without its Bcc header, leaving every other byte alone.
//
// Blind copy only works if the recipients are addressed but not disclosed: the SMTP
// envelope is read from the headers, so BuildMessage has to write Bcc, and the copy filed
// in Sent must not carry it. Two builds from one Compose would also differ in Date; this
// edits the bytes that were actually sent.
//
// It takes BuildMessage output, whose headers are CRLF-terminated and end at the first
// blank line, so a body line that happens to start with "Bcc:" is not a header and is
// left untouched. A folded value is removed with its continuation lines (#62).
func StripBcc(raw []byte) []byte {
	split := bytes.Index(raw, []byte("\r\n\r\n"))
	if split < 0 {
		return raw
	}

	lines := strings.Split(string(raw[:split]), "\r\n")
	kept := make([]string, 0, len(lines))
	skipping := false

	for _, line := range lines {
		if skipping && (strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t")) {
			continue
		}
		skipping = false

		if name, _, ok := strings.Cut(line, ":"); ok && strings.EqualFold(strings.TrimSpace(name), "Bcc") {
			skipping = true
			continue
		}

		kept = append(kept, line)
	}

	out := make([]byte, 0, len(raw))
	out = append(out, strings.Join(kept, "\r\n")...)
	out = append(out, raw[split:]...)

	return out
}
