package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeConfig writes body to a temp file with mode and returns its path.
func writeConfig(t *testing.T, body string, mode os.FileMode) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(body), mode); err != nil {
		t.Fatalf("writing config: %v", err)
	}

	return path
}

const validConfig = `
[general]
poll_interval = 30

[[accounts]]
name = "fastmail"
default = true
from = "Blaine <blaine@renderorange.com>"

[accounts.imap]
host = "imap.fastmail.com"
username = "blaine@renderorange.com"
password_command = "pass show mail/fastmail"

[[accounts]]
name = "work"

[accounts.imap]
host = "imap.example.com"
username = "blaine@example.com"
password = "secret"

[accounts.smtp]
host = "smtp.example.com"
`

func TestLoadValid(t *testing.T) {
	cfg, err := Load(writeConfig(t, validConfig, 0o600))
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	if len(cfg.Accounts) != 2 {
		t.Fatalf("accounts = %d, want 2", len(cfg.Accounts))
	}
	if cfg.General.PollInterval != 30 {
		t.Errorf("poll interval = %d, want 30", cfg.General.PollInterval)
	}
}

func TestLoadAppliesDefaults(t *testing.T) {
	cfg, err := Load(writeConfig(t, validConfig, 0o600))
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	fastmail := cfg.Accounts[0]
	if got, want := fastmail.IMAP.Port, DefaultIMAPPort; got != want {
		t.Errorf("imap port = %d, want %d", got, want)
	}
	if fastmail.IMAP.UseTLS == nil || !*fastmail.IMAP.UseTLS {
		t.Error("imap use_tls should default to true")
	}

	// SMTP inherits the IMAP host and credentials when it is unset.
	if got, want := fastmail.SMTP.Host, fastmail.IMAP.Host; got != want {
		t.Errorf("smtp host = %q, want inherited %q", got, want)
	}
	if got, want := fastmail.SMTP.Username, fastmail.IMAP.Username; got != want {
		t.Errorf("smtp username = %q, want inherited %q", got, want)
	}
	if got, want := fastmail.SMTP.PasswordCommand, fastmail.IMAP.PasswordCommand; got != want {
		t.Errorf("smtp password command = %q, want inherited %q", got, want)
	}
	if got, want := fastmail.SMTP.Port, DefaultSMTPPort; got != want {
		t.Errorf("smtp port = %d, want %d", got, want)
	}

	// An explicit SMTP host is kept.
	work := cfg.Accounts[1]
	if got, want := work.SMTP.Host, "smtp.example.com"; got != want {
		t.Errorf("smtp host = %q, want %q", got, want)
	}
}

func TestLoadDefaultsPollInterval(t *testing.T) {
	body := strings.Replace(validConfig, "poll_interval = 30", "", 1)

	cfg, err := Load(writeConfig(t, body, 0o600))
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if cfg.General.PollInterval != 60 {
		t.Errorf("poll interval = %d, want default 60", cfg.General.PollInterval)
	}
}

func TestLoadRejectsBadConfig(t *testing.T) {
	tests := []struct {
		name    string
		body    string
		wantErr string
	}{
		{
			name:    "no accounts",
			body:    "[general]\n",
			wantErr: "at least one",
		},
		{
			name:    "missing name",
			body:    "[[accounts]]\n[accounts.imap]\nhost = \"h\"\nusername = \"u\"\npassword = \"p\"\n",
			wantErr: "name is required",
		},
		{
			name: "duplicate name",
			body: `
[[accounts]]
name = "a"
[accounts.imap]
host = "h"
username = "u"
password = "p"

[[accounts]]
name = "a"
[accounts.imap]
host = "h2"
username = "u2"
password = "p2"
`,
			wantErr: "duplicate account name",
		},
		{
			name: "missing host",
			body: `
[[accounts]]
name = "a"
[accounts.imap]
username = "u"
password = "p"
`,
			wantErr: "imap.host is required",
		},
		{
			name: "missing username",
			body: `
[[accounts]]
name = "a"
[accounts.imap]
host = "h"
password = "p"
`,
			wantErr: "imap.username is required",
		},
		{
			name: "both credential sources",
			body: `
[[accounts]]
name = "a"
[accounts.imap]
host = "h"
username = "u"
password = "p"
password_command = "pass show x"
`,
			wantErr: "mutually exclusive",
		},
		{
			name: "no credential source",
			body: `
[[accounts]]
name = "a"
[accounts.imap]
host = "h"
username = "u"
`,
			wantErr: "imap.password or imap.password_command is required",
		},
		{
			name: "two defaults",
			body: `
[[accounts]]
name = "a"
default = true
[accounts.imap]
host = "h"
username = "u"
password = "p"

[[accounts]]
name = "b"
default = true
[accounts.imap]
host = "h"
username = "u"
password = "p"
`,
			wantErr: "only one account may set default",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Load(writeConfig(t, tc.body, 0o600))
			if err == nil {
				t.Fatal("Load returned no error")
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("error = %q, want it to contain %q", err, tc.wantErr)
			}
		})
	}
}

func TestLoadRejectsPublicPermissions(t *testing.T) {
	path := writeConfig(t, validConfig, 0o644)

	_, err := Load(path)
	if err == nil {
		t.Fatal("Load accepted a group-readable config")
	}
	if !strings.Contains(err.Error(), "mode") {
		t.Errorf("error = %q, want it to mention the file mode", err)
	}
}

func TestLoadUnsafeConfigSkipsPermissionCheck(t *testing.T) {
	body := strings.Replace(validConfig, "[general]", "[general]\nunsafe_config = true", 1)

	if _, err := Load(writeConfig(t, body, 0o644)); err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
}

func TestDefaultAccount(t *testing.T) {
	cfg, err := Load(writeConfig(t, validConfig, 0o600))
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	acct, err := cfg.Default()
	if err != nil {
		t.Fatalf("Default returned error: %v", err)
	}
	if acct.Name != "fastmail" {
		t.Errorf("default account = %q, want fastmail", acct.Name)
	}
}

func TestDefaultAccountFallsBackToFirst(t *testing.T) {
	body := strings.Replace(validConfig, "default = true\n", "", 1)
	cfg, err := Load(writeConfig(t, body, 0o600))
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	acct, err := cfg.Default()
	if err != nil {
		t.Fatalf("Default returned error: %v", err)
	}
	if acct.Name != "fastmail" {
		t.Errorf("default account = %q, want first account fastmail", acct.Name)
	}
}

func TestAccountLookup(t *testing.T) {
	cfg, err := Load(writeConfig(t, validConfig, 0o600))
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	if _, err := cfg.Account("work"); err != nil {
		t.Errorf("Account(work) returned error: %v", err)
	}
	if _, err := cfg.Account("nope"); err == nil {
		t.Error("Account(nope) returned no error")
	}
}

func TestSender(t *testing.T) {
	cfg, err := Load(writeConfig(t, validConfig, 0o600))
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	if got, want := cfg.Accounts[0].Sender(), "Blaine <blaine@renderorange.com>"; got != want {
		t.Errorf("sender = %q, want %q", got, want)
	}
	if got, want := cfg.Accounts[1].Sender(), "blaine@example.com"; got != want {
		t.Errorf("sender = %q, want login name %q", got, want)
	}
}

func TestLoadHideFolders(t *testing.T) {
	body := strings.Replace(validConfig,
		`password_command = "pass show mail/fastmail"`,
		`password_command = "pass show mail/fastmail"
hide_folders = ["Notes", "Receipts"]`, 1)

	cfg, err := Load(writeConfig(t, body, 0o600))
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	got := cfg.Accounts[0].IMAP.HideFolders
	if len(got) != 2 || got[0] != "Notes" || got[1] != "Receipts" {
		t.Errorf("hide_folders = %v, want [Notes Receipts]", got)
	}
}

func TestLoadStartFolder(t *testing.T) {
	body := strings.Replace(validConfig, `[[accounts]]
name = "fastmail"`, `[[accounts]]
name = "fastmail"
start_folder = "Work"`, 1)

	cfg, err := Load(writeConfig(t, body, 0o600))
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	got := cfg.Accounts[0].StartFolder
	if got == nil || *got != "Work" {
		t.Errorf("start_folder = %v, want Work", got)
	}
	if cfg.Accounts[1].StartFolder != nil {
		t.Error("unset start_folder should stay nil")
	}
}

func TestStartFolderName(t *testing.T) {
	work := "Work"
	disabled := ""

	tests := []struct {
		name string
		acct Account
		want string
	}{
		{"default", Account{}, "INBOX"},
		{"configured", Account{StartFolder: &work}, "Work"},
		{"disabled", Account{StartFolder: &disabled}, ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.acct.StartFolderName(); got != tc.want {
				t.Errorf("StartFolderName() = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestReplyPositionDefaultsToTop reads the field the composer reads, so removing the
// default fails here rather than silently changing where replies start.
func TestReplyPositionDefaultsToTop(t *testing.T) {
	cfg, err := Load(writeConfig(t, validConfig, 0o600))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if got := cfg.General.ReplyPosition; got != "top" {
		t.Errorf("ReplyPosition = %q, want %q", got, "top")
	}
	if cfg.General.ReplyAtBottom() {
		t.Error("ReplyAtBottom() is true for the default")
	}
}

// TestReplyPositionNormalizesCase proves the value is stored canonically, so the
// composer can compare it without knowing what the user typed.
func TestReplyPositionNormalizesCase(t *testing.T) {
	body := strings.Replace(validConfig, "poll_interval = 30",
		"poll_interval = 30\nreply_position = \"Bottom\"", 1)

	cfg, err := Load(writeConfig(t, body, 0o600))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if got := cfg.General.ReplyPosition; got != "bottom" {
		t.Errorf("ReplyPosition = %q, want %q", got, "bottom")
	}
	if !cfg.General.ReplyAtBottom() {
		t.Error("ReplyAtBottom() is false for reply_position = bottom")
	}
}

// TestReplyPositionRejectsUnknown proves a typo fails at load time and names both
// accepted values, rather than falling back to a default the user did not ask for.
func TestReplyPositionRejectsUnknown(t *testing.T) {
	body := strings.Replace(validConfig, "poll_interval = 30",
		"poll_interval = 30\nreply_position = \"middle\"", 1)

	_, err := Load(writeConfig(t, body, 0o600))
	if err == nil {
		t.Fatal("Load accepted reply_position = middle")
	}

	for _, want := range []string{"reply_position", "top", "bottom"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not name %q", err, want)
		}
	}
}

// TestLoadLayout proves the four sizing keys reach the config, both the percentages and
// the absolute widths, since the TUI treats them differently.
func TestLoadLayout(t *testing.T) {
	body := validConfig + `
[layout]
folders_percent = 10
messages_percent = 25
folders_width = 30
messages_width = 60
`

	cfg, err := Load(writeConfig(t, body, 0o600))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	want := Layout{FoldersPercent: 10, MessagesPercent: 25, FoldersWidth: 30, MessagesWidth: 60}
	if got := cfg.Layout; got != want {
		t.Errorf("Layout = %+v, want %+v", got, want)
	}
}

// TestLoadLayoutDefaultsToAutomatic proves an absent [layout] section leaves every key at
// zero, which the TUI reads as "use the built-in rules". A default filled in here would
// silently pin the sizing and make the automatic rules unreachable.
func TestLoadLayoutDefaultsToAutomatic(t *testing.T) {
	cfg, err := Load(writeConfig(t, validConfig, 0o600))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if got := cfg.Layout; got != (Layout{}) {
		t.Errorf("Layout = %+v, want the zero value", got)
	}
}

// TestLoadRejectsBadLayout proves the ranges a config file can be judged on are checked at
// load: negative sizes, and a percentage that leaves the reader nothing. A width that is
// merely too small for the terminal is not a config error; the TUI clamps that.
func TestLoadRejectsBadLayout(t *testing.T) {
	tests := []struct {
		name    string
		section string
		wantErr string
	}{
		{"negative folders width", "folders_width = -1", "folders_width"},
		{"negative messages width", "messages_width = -10", "messages_width"},
		{"negative messages percent", "messages_percent = -5", "messages_percent"},
		{"folders percent too large", "folders_percent = 91", "folders_percent"},
		{"messages percent too large", "messages_percent = 100", "messages_percent"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			body := validConfig + "\n[layout]\n" + tc.section + "\n"

			_, err := Load(writeConfig(t, body, 0o600))
			if err == nil {
				t.Fatalf("Load accepted %s", tc.section)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("error %q does not name %q", err, tc.wantErr)
			}
		})
	}
}

// TestLoadAcceptsLayoutBoundaries proves the largest legal percentage loads, so the range
// is inclusive at the top rather than rejecting the documented maximum.
func TestLoadAcceptsLayoutBoundaries(t *testing.T) {
	body := validConfig + "\n[layout]\nmessages_percent = 90\nfolders_width = 1\n"

	cfg, err := Load(writeConfig(t, body, 0o600))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := cfg.Layout.MessagesPercent; got != 90 {
		t.Errorf("MessagesPercent = %d, want 90", got)
	}
	if got := cfg.Layout.FoldersWidth; got != 1 {
		t.Errorf("FoldersWidth = %d, want 1: a too-small width is the TUI's to clamp", got)
	}
}

// TestIMAPDefaultPortFollowsTLSMode pins #60's other half: use_tls = false with no port
// means 143, not 993, so the setting does not point STARTTLS at the TLS port.
func TestIMAPDefaultPortFollowsTLSMode(t *testing.T) {
	no := false

	cfg := &Config{Accounts: []Account{
		{Name: "implicit", IMAP: IMAP{Host: "a.example", Username: "u", Password: "p"}},
		{Name: "starttls", IMAP: IMAP{Host: "b.example", Username: "u", Password: "p", UseTLS: &no}},
		{Name: "explicit", IMAP: IMAP{Host: "c.example", Username: "u", Password: "p", Port: 1993, UseTLS: &no}},
	}}
	cfg.applyDefaults()

	for i, want := range []int{DefaultIMAPPort, DefaultIMAPStartTLSPort, 1993} {
		if got := cfg.Accounts[i].IMAP.Port; got != want {
			t.Errorf("%s: port = %d, want %d", cfg.Accounts[i].Name, got, want)
		}
	}
}
