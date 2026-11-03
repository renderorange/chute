package mail

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/emersion/go-imap/v2"
)

// ErrEmptySearch reports a prompt with nothing in it. The UI rejects it before opening a
// connection, so this is a belt-and-braces refusal for other callers.
var ErrEmptySearch = errors.New("mail: empty search")

// SearchCriteria turns a prompt into IMAP SEARCH criteria.
//
// The grammar is deliberately small, because raw SEARCH syntax is powerful and hostile:
//
//   - a bare word searches the whole message (IMAP TEXT: headers and body);
//   - from:VALUE searches the From header, subject:VALUE the Subject, text:VALUE the whole
//     message and body:VALUE the body only;
//   - since:YYYY-MM-DD searches messages received on or after that day.
//
// Words are ANDed, because that is what IMAP does with several criteria; a prefix with an
// empty value and an unknown prefix fall back to a literal search, so a typo finds something
// rather than silently matching everything. The second result is false when the input has no
// searchable words.
func SearchCriteria(input string) (imap.SearchCriteria, bool) {
	var criteria imap.SearchCriteria
	found := false

	for _, token := range strings.Fields(input) {
		key, value, prefixed := strings.Cut(token, ":")
		if !prefixed || value == "" {
			criteria.Text = append(criteria.Text, token)
			found = true

			continue
		}

		switch strings.ToLower(key) {
		case "from":
			criteria.Header = append(criteria.Header, imap.SearchCriteriaHeaderField{Key: "From", Value: value})

		case "subject":
			criteria.Header = append(criteria.Header, imap.SearchCriteriaHeaderField{Key: "Subject", Value: value})

		case "text":
			criteria.Text = append(criteria.Text, value)

		case "body":
			criteria.Body = append(criteria.Body, value)

		case "since":
			day, err := time.Parse("2006-01-02", value)
			if err != nil {
				// A malformed date searches for the literal, which surfaces the typo in the
				// result count instead of silently dropping the narrowing.
				criteria.Text = append(criteria.Text, token)
			} else {
				criteria.Since = day
			}

		default:
			criteria.Text = append(criteria.Text, token)
		}

		found = true
	}

	return criteria, found
}

// Search runs one UID SEARCH and one FETCH for the matches. It never reloads the folder's
// envelopes: the whole point is finding mail the list does not hold.
func (c *Client) Search(folder string, criteria imap.SearchCriteria) ([]Message, error) {
	if _, err := c.conn.Select(folder, nil).Wait(); err != nil {
		return nil, fmt.Errorf("mail: account %q: opening %q: %w", c.account, folder, err)
	}

	data, err := c.conn.UIDSearch(&criteria, nil).Wait()
	if err != nil {
		return nil, fmt.Errorf("mail: account %q: searching %q: %w", c.account, folder, err)
	}

	uids := data.AllUIDs()
	if len(uids) == 0 {
		return nil, nil
	}

	var set imap.UIDSet
	for _, uid := range uids {
		set.AddNum(uid)
	}

	cmd := c.conn.Fetch(set, listFetchOptions())
	defer cmd.Close()

	buffers, err := cmd.Collect()
	if err != nil {
		return nil, fmt.Errorf("mail: account %q: fetching search results from %q: %w", c.account, folder, err)
	}

	return messagesFromBuffers(buffers), nil
}

// Search finds messages in one folder through the server. Only SEARCH and one FETCH reach the
// wire: the folder's envelope list is not re-downloaded.
func (s *Session) Search(folder, query string) ([]Message, error) {
	criteria, ok := SearchCriteria(query)
	if !ok {
		return nil, ErrEmptySearch
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	client, err := s.acquireLocked()
	if err != nil {
		return nil, err
	}

	messages, err := client.Search(folder, criteria)
	if err != nil {
		s.resetLocked()
		return nil, err
	}

	return messages, nil
}
