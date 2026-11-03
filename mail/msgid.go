package mail

import "strings"

// Message-IDs cross three shapes in this package:
//
//   - IMAP envelopes carry them without angle brackets (go-imap's Envelope doc comment
//     says so explicitly);
//   - RFC 5322 headers require exactly one pair of angle brackets around every id;
//   - the internal values (Message.MessageID, Body.References, Compose.MessageID,
//     Compose.InReplyTo, Compose.References) use the bare form, so comparisons and
//     chains never mix shapes. #71 was the bug from mixing them.
//
// bareID strips whitespace and any surrounding brackets, and headerID adds one pair, so
// both are idempotent: an old draft or a hand-built Compose cannot produce <<id>>.
func bareID(id string) string {
	return strings.Trim(strings.TrimSpace(id), "<>")
}

// bareIDs applies bareID to a chain, dropping empty entries.
func bareIDs(ids []string) []string {
	bare := make([]string, 0, len(ids))

	for _, id := range ids {
		if one := bareID(id); one != "" {
			bare = append(bare, one)
		}
	}

	return bare
}

// headerID renders a bare id as an RFC 5322 msg-id, or "" for an empty input.
func headerID(id string) string {
	bare := bareID(id)
	if bare == "" {
		return ""
	}

	return "<" + bare + ">"
}

// headerIDs renders a chain as msg-ids, dropping empty entries.
func headerIDs(ids []string) []string {
	header := make([]string, 0, len(ids))

	for _, id := range ids {
		if one := headerID(id); one != "" {
			header = append(header, one)
		}
	}

	return header
}
