package mail

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/renderorange/chute/config"
)

// TestSearchCriteriaGrammar pins the small grammar: a bare word searches the whole message,
// the four prefixes narrow it, and a typo searches literally rather than matching everything.
func TestSearchCriteriaGrammar(t *testing.T) {
	tests := []struct {
		name  string
		input string
		check func(t *testing.T, c imap.SearchCriteria)
		found bool
	}{
		{
			name:  "bare word is a text search",
			input: "invoice",
			found: true,
			check: func(t *testing.T, c imap.SearchCriteria) {
				if got := strings.Join(c.Text, " "); got != "invoice" {
					t.Errorf("Text = %q, want invoice", got)
				}
			},
		},
		{
			name:  "words are ANDed",
			input: "invoice march",
			found: true,
			check: func(t *testing.T, c imap.SearchCriteria) {
				if got := strings.Join(c.Text, " "); got != "invoice march" {
					t.Errorf("Text = %q, want both words", got)
				}
			},
		},
		{
			name:  "from and subject become header fields",
			input: "from:alice subject:report",
			found: true,
			check: func(t *testing.T, c imap.SearchCriteria) {
				if len(c.Header) != 2 {
					t.Fatalf("Header = %v, want From and Subject", c.Header)
				}
				if c.Header[0].Key != "From" || c.Header[0].Value != "alice" {
					t.Errorf("first header = %v, want From alice", c.Header[0])
				}
				if c.Header[1].Key != "Subject" || c.Header[1].Value != "report" {
					t.Errorf("second header = %v, want Subject report", c.Header[1])
				}
			},
		},
		{
			name:  "body and text are distinct",
			input: "body:invoice text:march",
			found: true,
			check: func(t *testing.T, c imap.SearchCriteria) {
				if strings.Join(c.Body, " ") != "invoice" || strings.Join(c.Text, " ") != "march" {
					t.Errorf("Body = %v, Text = %v", c.Body, c.Text)
				}
			},
		},
		{
			name:  "since parses a day",
			input: "since:2026-01-02",
			found: true,
			check: func(t *testing.T, c imap.SearchCriteria) {
				if want := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC); !c.Since.Equal(want) {
					t.Errorf("Since = %v, want %v", c.Since, want)
				}
			},
		},
		{
			name:  "a malformed date searches literally",
			input: "since:last-week",
			found: true,
			check: func(t *testing.T, c imap.SearchCriteria) {
				if !c.Since.IsZero() {
					t.Errorf("Since = %v, want zero", c.Since)
				}
				if got := strings.Join(c.Text, " "); got != "since:last-week" {
					t.Errorf("Text = %q, want the literal token", got)
				}
			},
		},
		{
			name:  "an unknown prefix searches literally",
			input: "label:work",
			found: true,
			check: func(t *testing.T, c imap.SearchCriteria) {
				if got := strings.Join(c.Text, " "); got != "label:work" {
					t.Errorf("Text = %q, want the literal token", got)
				}
			},
		},
		{
			name:  "whitespace only finds nothing",
			input: "   ",
			found: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			criteria, found := SearchCriteria(tc.input)

			if found != tc.found {
				t.Fatalf("found = %v, want %v", found, tc.found)
			}
			if tc.check != nil {
				tc.check(t, criteria)
			}
		})
	}
}

// countClientSearches counts the UID SEARCH commands the client sent. The completion line
// ("OK UID SEARCH completed") contains the same words, so the tag position decides.
func countClientSearches(protocolLog string) int {
	count := 0

	for _, line := range strings.Split(protocolLog, "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 3 && fields[1] == "UID" && strings.EqualFold(fields[2], "SEARCH") {
			count++
		}
	}

	return count
}

// TestClientSearchUsesOneSearchAndOneFetch is #68's cost claim, and the reason it exists: body
// text the folder load never fetched is found without reloading the folder's envelopes.
func TestClientSearchUsesOneSearchAndOneFetch(t *testing.T) {
	server := newTestServer(t, testCaps())
	log := &syncBuffer{}

	client, err := connectWith(dialInsecure, server.account(), "secret", WithDebugWriter(log))
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer client.Close()

	server.appendMessage(t, "INBOX", "ordinary subject")
	server.appendReferencesMessage(t, "INBOX", "another", "other@example.com", "<root@example.com>")

	// A body-only word: the envelope fetch never sees it.
	raw := "From: alice@example.com\r\n" +
		"To: tester@example.com\r\n" +
		"Subject: quarterly\r\n" +
		"Date: Mon, 02 Jan 2006 15:04:05 -0700\r\n" +
		"\r\n" +
		"the invoice is attached\r\n"
	if _, err := server.user.Append("INBOX", strings.NewReader(raw), &imap.AppendOptions{}); err != nil {
		t.Fatalf("append: %v", err)
	}

	criteria, ok := SearchCriteria("invoice")
	if !ok {
		t.Fatal("SearchCriteria found nothing")
	}

	messages, err := client.Search("INBOX", criteria)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}

	if len(messages) != 1 {
		t.Fatalf("found %d messages, want the one with the body word: %v", len(messages), messages)
	}
	if got := messages[0].Subject(); got != "quarterly" {
		t.Errorf("subject = %q, want quarterly", got)
	}

	if got := countClientSearches(log.String()); got != 1 {
		t.Errorf("sent %d UID SEARCH commands, want 1:\n%s", got, log.String())
	}
	if got := countClientFetches(log.String()); got != 1 {
		t.Errorf("sent %d FETCH commands, want 1:\n%s", got, log.String())
	}
}

// TestClientSearchNarrowsByHeader proves a from: query matches that header and not the body.
func TestClientSearchNarrowsByHeader(t *testing.T) {
	server := newTestServer(t, testCaps())
	client := server.connect(t)

	server.appendMessage(t, "INBOX", "from sender")
	server.appendReferencesMessage(t, "INBOX", "from alice", "alice@example.com", "<x@example.com>")

	criteria, _ := SearchCriteria("subject:alice")
	messages, err := client.Search("INBOX", criteria)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}

	if len(messages) != 1 || messages[0].Subject() != "from alice" {
		t.Errorf("messages = %v, want only the subject match", messages)
	}
}

// TestSessionSearchRejectsAnEmptyQuery keeps an empty prompt from becoming a folder-wide
// GETALL.
func TestSessionSearchRejectsAnEmptyQuery(t *testing.T) {
	client := &fakeClient{}
	session := newMoveSession(t, config.Account{Name: "fastmail"}, client)

	if _, err := session.Search("INBOX", "   "); !errors.Is(err, ErrEmptySearch) {
		t.Fatalf("error = %v, want ErrEmptySearch", err)
	}
	if client.requests != 0 {
		t.Errorf("requests = %d, want none", client.requests)
	}
}

// TestSessionSearchHandsTheFolderAndCriteriaToTheClient covers the seam.
func TestSessionSearchHandsTheFolderAndCriteriaToTheClient(t *testing.T) {
	client := &fakeClient{messages: []Message{{UID: 7}}}
	session := newMoveSession(t, config.Account{Name: "fastmail"}, client)

	messages, err := session.Search("Archive", "from:alice")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if client.searchFolder != "Archive" {
		t.Errorf("folder = %q, want Archive", client.searchFolder)
	}
	if len(client.searchCriteria.Header) != 1 || client.searchCriteria.Header[0].Value != "alice" {
		t.Errorf("criteria = %+v, want a From header for alice", client.searchCriteria)
	}
	if len(messages) != 1 || messages[0].UID != 7 {
		t.Errorf("messages = %v, want the client's UID 7", messages)
	}
}
