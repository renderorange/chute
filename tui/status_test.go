package tui

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/renderorange/chute/mail"
)

// timedOut stands in for a net.Error whose deadline passed, so the timeout phrase can be
// tested without waiting for a socket.
type timedOut struct{}

func (timedOut) Error() string   { return "i/o timeout" }
func (timedOut) Timeout() bool   { return true }
func (timedOut) Temporary() bool { return true }

// TestStatusTextIsAWhitelist is the policy table in tui/status.go: known failures get
// one sentence, unknown ones keep everything so the detail is never lost.
func TestStatusTextIsAWhitelist(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{
			name: "timeout",
			err:  fmt.Errorf("mail: polling: %w", timedOut{}),
			want: "server timed out",
		},
		{
			name: "auth",
			err:  fmt.Errorf("%w: account %q: login as blaine failed", mail.ErrAuth, "fastmail"),
			want: "login rejected: check the password",
		},
		{
			name: "connect",
			err:  fmt.Errorf("%w: account %q: connecting to imap.fastmail.com:993", mail.ErrConnect, "fastmail"),
			want: "cannot connect to the mail server",
		},
		{
			name: "move support",
			err:  fmt.Errorf("%w: account %q", mail.ErrNoMoveSupport, "fastmail"),
			want: "server cannot move mail",
		},
		{
			name: "unknown wrapped keeps its text",
			err:  fmt.Errorf("mail: account %q: opening %q: %w", "fastmail", "INBOX", errors.New("boom")),
			want: `mail: account "fastmail": opening "INBOX": boom`,
		},
		{
			name: "unknown plain keeps its text",
			err:  errors.New("DNS exploded"),
			want: "DNS exploded",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := statusText(tc.err); got != tc.want {
				t.Errorf("statusText(%v) = %q, want %q", tc.err, got, tc.want)
			}
		})
	}
}

// TestTimeoutOutranksConnect proves the most specific phrase wins when a dial timeout is
// also a connect failure.
func TestTimeoutOutranksConnect(t *testing.T) {
	err := fmt.Errorf("%w: %w", mail.ErrConnect, timedOut{})

	if got := statusText(err); got != "server timed out" {
		t.Errorf("statusText = %q, want the timeout phrase", got)
	}
}

// TestLoaderErrorsUseThePhrases applies the whitelist at the points that put a loader
// error on the status bar, so a new call site cannot bypass it.
func TestLoaderErrorsUseThePhrases(t *testing.T) {
	authErr := func() error {
		return fmt.Errorf("%w: account %q: login as blaine failed", mail.ErrAuth, "fastmail")
	}

	t.Run("folders", func(t *testing.T) {
		m := sized(105, 40)

		next, _ := m.Update(foldersMsg{account: "fastmail", err: authErr()})
		m = next.(Model)

		if m.status != "login rejected: check the password" {
			t.Errorf("status = %q", m.status)
		}
		if m.state != stateFailed {
			t.Errorf("state = %v, want failed", m.state)
		}
	})

	t.Run("messages", func(t *testing.T) {
		m := openInbox(t, withInbox(t, sampleMessages(2), nil))

		next, _ := m.Update(messagesMsg{account: "fastmail", folder: "INBOX", err: authErr()})
		m = next.(Model)

		if m.status != "login rejected: check the password" {
			t.Errorf("status = %q", m.status)
		}
	})

	t.Run("body", func(t *testing.T) {
		m := openInbox(t, withInbox(t, sampleMessages(2), nil))

		next, _ := m.Update(bodyMsg{account: "fastmail", folder: "INBOX", uid: 1, err: authErr()})
		m = next.(Model)

		if m.status != "login rejected: check the password" {
			t.Errorf("status = %q", m.status)
		}
	})

	t.Run("flags", func(t *testing.T) {
		m := openInbox(t, withInbox(t, sampleMessages(2), nil))

		next, _ := m.Update(flagsMsg{account: "fastmail", folder: "INBOX", err: authErr()})
		m = next.(Model)

		if m.status != "login rejected: check the password" {
			t.Errorf("status = %q", m.status)
		}
	})

	t.Run("poll", func(t *testing.T) {
		m := openInbox(t, withInbox(t, sampleMessages(2), nil))

		next, _ := m.Update(pollMsg{account: "fastmail", folder: "INBOX", err: authErr()})
		m = next.(Model)

		if m.status != "login rejected: check the password" {
			t.Errorf("status = %q", m.status)
		}
	})

	t.Run("uids", func(t *testing.T) {
		m := openInbox(t, withInbox(t, sampleMessages(2), nil))

		next, _ := m.Update(uidsMsg{account: "fastmail", folder: "INBOX", err: authErr()})
		m = next.(Model)

		if m.status != "login rejected: check the password" {
			t.Errorf("status = %q", m.status)
		}
	})

	t.Run("attachment", func(t *testing.T) {
		m := sized(105, 40)

		next, _ := m.Update(attachmentSavedMsg{path: "/tmp/x", err: authErr()})
		m = next.(Model)

		if m.status != "login rejected: check the password" {
			t.Errorf("status = %q", m.status)
		}
	})

	t.Run("delete without trash", func(t *testing.T) {
		m := openInbox(t, withInbox(t, sampleMessages(2), nil))

		next, _ := m.Update(deleteMsg{
			account: "fastmail",
			folder:  "INBOX",
			err:     fmt.Errorf("%w: account %q", mail.ErrNoRoleFolder, "fastmail"),
		})
		m = next.(Model)

		if m.status != "no trash folder configured" {
			t.Errorf("status = %q", m.status)
		}
	})

	t.Run("move connection failure", func(t *testing.T) {
		got := moveStatus(moveMsg{
			role: mail.RoleArchive,
			err:  fmt.Errorf("%w: account %q", mail.ErrConnect, "fastmail"),
		})

		if got != "cannot connect to the mail server" {
			t.Errorf("moveStatus = %q", got)
		}
	})
}

// TestComposeErrorsKeepTheirPrefix proves the composer's context survives the mapping:
// "send failed" and "draft not saved" stay, the wrapped chain becomes the phrase.
func TestComposeErrorsKeepTheirPrefix(t *testing.T) {
	authErr := fmt.Errorf("%w: account %q", mail.ErrAuth, "fastmail")

	t.Run("send", func(t *testing.T) {
		m := sized(105, 40)
		m.applySendResult(sendResultMsg{account: "fastmail", sent: false, err: authErr})

		if want := "send failed: login rejected: check the password"; m.status != want {
			t.Errorf("status = %q, want %q", m.status, want)
		}
	})

	t.Run("draft save", func(t *testing.T) {
		m := sized(105, 40)
		m.applyDraftSaved(draftSavedMsg{account: "fastmail", err: authErr})

		if want := "draft not saved: login rejected: check the password"; m.status != want {
			t.Errorf("status = %q, want %q", m.status, want)
		}
	})

	t.Run("draft open", func(t *testing.T) {
		m := sized(105, 40)
		m.applyComposeLoaded(composeLoadedMsg{account: "fastmail", err: authErr})

		if want := "draft not opened: login rejected: check the password"; m.status != want {
			t.Errorf("status = %q, want %q", m.status, want)
		}
	})
}

// TestUnknownComposeErrorKeepsItsText proves an unanticipated failure is not flattened
// into a generic phrase.
func TestUnknownComposeErrorKeepsItsText(t *testing.T) {
	m := sized(105, 40)
	boom := errors.New("mail: account \"fastmail\": data: broken pipe")
	m.applySendResult(sendResultMsg{account: "fastmail", sent: false, err: boom})

	if got, want := m.status, "send failed: "+boom.Error(); got != want {
		t.Errorf("status = %q, want %q", got, want)
	}
	if !strings.Contains(m.status, "broken pipe") {
		t.Error("the unrecognised detail was dropped")
	}
}
