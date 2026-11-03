package tui

import (
	"strings"
	"testing"

	"github.com/emersion/go-imap/v2"
	"github.com/renderorange/chute/mail"
)

// withSettings builds a model whose config exercises every value the report names.
func withSettings(t *testing.T) Model {
	t.Helper()

	no := false

	cfg := testConfig()
	cfg.General.PollInterval = 30
	cfg.General.DownloadDir = "/tmp/attachments"
	cfg.General.ReplyPosition = "bottom"
	cfg.Layout.FoldersWidth = 28
	cfg.Layout.MessagesPercent = 55
	cfg.List.Columns = []string{"from", "subject", "size", "date"}

	cfg.Accounts[0].From = "Me <me@example.org>"
	cfg.Accounts[0].StartFolder = nil
	cfg.Accounts[0].IMAP.Host = "imap.example.org"
	cfg.Accounts[0].IMAP.Port = 993
	cfg.Accounts[0].IMAP.Password = "hunter2-secret"
	cfg.Accounts[0].SMTP.Host = "smtp.example.org"
	cfg.Accounts[0].SMTP.Port = 587
	cfg.Accounts[0].SMTP.UseTLS = &no
	cfg.Accounts[0].SMTP.PasswordCommand = "pass show hunter2-secret"

	m := sizedWith(cfg, 140, 40)
	m.folders = []mail.Folder{
		{Name: "INBOX", Delimiter: '/'},
		{Name: "Archive", Delimiter: '/', Attrs: []imap.MailboxAttr{imap.MailboxAttrArchive}},
		{Name: "Trash", Delimiter: '/', Attrs: []imap.MailboxAttr{imap.MailboxAttrTrash}},
	}
	m = m.WithPaths("/home/me/.config/chute/config.toml", "")

	return m
}

// TestSettingsOpensAndCloses covers the key and that closing runs nothing else.
func TestSettingsOpensAndCloses(t *testing.T) {
	m := withSettings(t)
	m.filter = "unrelated"

	m = press(t, m, keyMsg(","))

	if !m.settingsOpen {
		t.Fatal(", did not open the settings report")
	}

	m = press(t, m, namedKey("esc"))

	if m.settingsOpen {
		t.Error("esc left the report open")
	}
	if m.filter != "unrelated" {
		t.Errorf("filter = %q: closing the report ran another action", m.filter)
	}
}

// TestSettingsShowsTheEffectiveConfiguration is the point of the screen: the values in force,
// with the source of the derived ones.
func TestSettingsShowsTheEffectiveConfiguration(t *testing.T) {
	m := press(t, withSettings(t), keyMsg(","))

	// The visible window covers the top of the report; the tail is reached by scrolling, so
	// the content check reads the whole report and the frame test below covers what is drawn.
	view := m.View() + "\n" + strings.Join(m.settingsLines(), "\n")

	for _, want := range []string{
		"chute test",
		"account: fastmail",
		"Me <me@example.org>",
		"imap.example.org:993 (implicit TLS)",
		"smtp.example.org:587 (STARTTLS when offered)",
		"start folder: INBOX (default)",
		"hidden folders: none",
		"roles",
		"Archive",
		"Trash",
		"poll interval: 30 s",
		"/tmp/attachments",
		"reply position: bottom",
		"configured width",
		"configured percent",
		"the rest",
		"from, subject, size, date",
		"? for the effective map",
		"/home/me/.config/chute/config.toml",
		"state.toml",
		"off (-debug to enable)",
	} {
		if !strings.Contains(view, want) {
			t.Errorf("the report is missing %q:\n%s", want, view)
		}
	}
}

// TestSettingsShowsAMissingRole keeps the common support question answerable: a role with no
// mailbox says so instead of showing a stale name.
func TestSettingsShowsAMissingRole(t *testing.T) {
	m := withSettings(t)
	m.folders = []mail.Folder{{Name: "INBOX", Delimiter: '/'}}

	view := press(t, m, keyMsg(",")).View()

	if !strings.Contains(view, "not found") {
		t.Errorf("the report does not name an unresolvable role:\n%s", view)
	}
}

// TestSettingsNeverShowsCredentials is the security criterion: the password and the command
// string may exist in the config, but not on this screen.
func TestSettingsNeverShowsCredentials(t *testing.T) {
	m := withSettings(t)

	for _, frame := range []struct {
		name string
		view string
	}{
		{"top", press(t, m, keyMsg(",")).View()},
	} {
		for _, secret := range []string{"hunter2-secret", "pass show"} {
			if strings.Contains(frame.view, secret) {
				t.Errorf("%s: the report shows %q", frame.name, secret)
			}
		}
	}

	// Scrolling must not reveal it either.
	for i := 0; i < 30; i++ {
		m = press(t, m, namedKey("down"))
	}

	view := m.View()
	for _, secret := range []string{"hunter2-secret", "pass show"} {
		if strings.Contains(view, secret) {
			t.Errorf("scrolled: the report shows %q", secret)
		}
	}
}

// TestSettingsFitsAndScrollsAtTheMinimum pins the frame and the scroll keys.
func TestSettingsFitsAndScrollsAtTheMinimum(t *testing.T) {
	m := loaded(t, 60, 20, []mail.Folder{{Name: "INBOX", Delimiter: '/'}}, nil)
	m = openInboxAnyMode(t, m)

	m = press(t, m, keyMsg(","))
	assertExactFrame(t, m, "settings")

	first := m.View()
	m = press(t, m, namedKey("down"))

	if m.settingsScroll != 1 {
		t.Errorf("scroll = %d, want 1", m.settingsScroll)
	}
	if m.View() == first {
		t.Error("scrolling did not move the report")
	}

	m = press(t, m, namedKey("up"))
	if m.settingsScroll != 0 {
		t.Errorf("scroll = %d, want 0 after up", m.settingsScroll)
	}

	m = press(t, m, namedKey("up"))
	if m.settingsScroll != 0 {
		t.Errorf("scroll = %d, want the top and no further", m.settingsScroll)
	}
}

// TestSettingsKeyCanBeRebound is the M6 property for the new action.
func TestSettingsKeyCanBeRebound(t *testing.T) {
	m := withSettings(t)

	keys := defaultBindings()
	keys[actionSettings] = []string{"ctrl+,"}
	m.keys = keys

	m = press(t, m, keyMsg("ctrl+,"))
	if !m.settingsOpen {
		t.Error("the rebound key did not open the report")
	}

	m.settingsOpen = false
	m = press(t, m, keyMsg(","))
	if m.settingsOpen {
		t.Error("the replaced default still opened the report")
	}
}
