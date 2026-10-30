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
