// Package config loads and validates the chute account configuration.
//
// The file is TOML and holds one or more accounts. Credentials come either from a
// plaintext password or from a password command; the command form is preferred.
package config

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/BurntSushi/toml"
)

// Default ports for the two protocols chute speaks.
const (
	DefaultIMAPPort = 993
	DefaultSMTPPort = 465
)

// privateMode is the only file mode accepted for a config holding credentials.
const privateMode = 0o600

// Config is the whole configuration file.
type Config struct {
	General  General   `toml:"general"`
	Layout   Layout    `toml:"layout"`
	Accounts []Account `toml:"accounts"`
}

// General holds settings that apply to every account.
type General struct {
	// UnsafeConfig skips the 0600 permission check. It exists as an escape hatch
	// for people deliberately keeping credentials elsewhere.
	UnsafeConfig bool `toml:"unsafe_config"`

	// PollInterval is the seconds between incremental new-mail checks.
	PollInterval int `toml:"poll_interval"`

	// DownloadDir is where attachments are saved. A leading ~ is expanded; an
	// empty value means ~/Downloads.
	DownloadDir string `toml:"download_dir"`

	// ReplyPosition places the cursor when composing a reply: "top" writes above
	// the quoted original, "bottom" below it. Normalized to lower case on load.
	ReplyPosition string `toml:"reply_position"`
}

// ReplyAtBottom reports whether replies start below the quoted original.
func (g General) ReplyAtBottom() bool {
	return g.ReplyPosition == ReplyPositionBottom
}

// Accepted reply_position values.
const (
	ReplyPositionTop    = "top"
	ReplyPositionBottom = "bottom"
)

// LayoutMaxPercent is the largest share of the content width one pane may take. The reader
// needs the rest, so 100 is not a legal setting.
const LayoutMaxPercent = 90

// Layout sizes the three panels. Every field's zero value means automatic: the TUI applies
// its built-in rules. A non-zero width wins over the matching percentage for that pane.
//
// A width that is too small for the terminal is not a load error, because the same file is
// used at 60, 105 and 200 columns; the TUI clamps it to the pane's minimum.
type Layout struct {
	// FoldersPercent sizes the folder pane as a percentage of the content width.
	FoldersPercent int `toml:"folders_percent"`

	// MessagesPercent sizes the message list as a percentage of the content width.
	MessagesPercent int `toml:"messages_percent"`

	// FoldersWidth and MessagesWidth are absolute column counts.
	FoldersWidth  int `toml:"folders_width"`
	MessagesWidth int `toml:"messages_width"`
}

// validate checks the ranges that do not depend on the terminal size.
func (l Layout) validate() error {
	percents := []struct {
		name  string
		value int
	}{
		{"folders_percent", l.FoldersPercent},
		{"messages_percent", l.MessagesPercent},
	}

	for _, p := range percents {
		if p.value < 0 || p.value > LayoutMaxPercent {
			return fmt.Errorf("config: layout.%s is %d, want 0..%d (0 means automatic)",
				p.name, p.value, LayoutMaxPercent)
		}
	}

	widths := []struct {
		name  string
		value int
	}{
		{"folders_width", l.FoldersWidth},
		{"messages_width", l.MessagesWidth},
	}

	for _, w := range widths {
		if w.value < 0 {
			return fmt.Errorf("config: layout.%s is %d, want a non-negative number of columns",
				w.name, w.value)
		}
	}

	return nil
}

// Account is one mail account.
type Account struct {
	Name    string `toml:"name"`
	Default bool   `toml:"default"`

	// From is the address used in the From header, for accounts whose login
	// differs from the address they send as.
	From string `toml:"from"`

	// StartFolder is opened automatically after the first folder load. Unset
	// means the account's INBOX; an empty string disables the automatic open.
	StartFolder *string `toml:"start_folder"`

	IMAP IMAP `toml:"imap"`
	SMTP SMTP `toml:"smtp"`
}

// IMAP is the incoming server for an account.
type IMAP struct {
	Host            string `toml:"host"`
	Port            int    `toml:"port"`
	Username        string `toml:"username"`
	Password        string `toml:"password"`
	PasswordCommand string `toml:"password_command"`
	UseTLS          *bool  `toml:"use_tls"`

	// Sent, Drafts, Trash, Archive and Junk override IMAP special-use detection.
	Sent    string `toml:"sent"`
	Drafts  string `toml:"drafts"`
	Trash   string `toml:"trash"`
	Archive string `toml:"archive"`
	Junk    string `toml:"junk"`

	// HideFolders lists mailbox names to leave out of the folder list. Their
	// subfolders are hidden too; matching is case-insensitive.
	HideFolders []string `toml:"hide_folders"`
}

// SMTP is the outgoing server for an account.
type SMTP struct {
	Host            string `toml:"host"`
	Port            int    `toml:"port"`
	Username        string `toml:"username"`
	Password        string `toml:"password"`
	PasswordCommand string `toml:"password_command"`
	UseTLS          *bool  `toml:"use_tls"`
}

// Load reads the config file, applies defaults, and validates it.
func Load(path string) (*Config, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("config: %w", err)
	}

	var cfg Config
	if _, err := toml.DecodeFile(path, &cfg); err != nil {
		return nil, fmt.Errorf("config: parsing %s: %w", path, err)
	}

	if !cfg.General.UnsafeConfig {
		mode := info.Mode().Perm()
		if mode != privateMode {
			return nil, fmt.Errorf("config: %s has mode %04o, want %04o (set general.unsafe_config = true to override)",
				path, mode, privateMode)
		}
	}

	cfg.applyDefaults()

	if err := cfg.validate(); err != nil {
		return nil, err
	}

	return &cfg, nil
}

// applyDefaults fills in ports and the TLS default for every account.
func (c *Config) applyDefaults() {
	if c.General.PollInterval == 0 {
		c.General.PollInterval = 60
	}

	if c.General.ReplyPosition == "" {
		c.General.ReplyPosition = ReplyPositionTop
	}
	c.General.ReplyPosition = strings.ToLower(strings.TrimSpace(c.General.ReplyPosition))

	for i := range c.Accounts {
		is := &c.Accounts[i].IMAP
		if is.Port == 0 {
			is.Port = DefaultIMAPPort
		}
		if is.UseTLS == nil {
			is.UseTLS = boolPtr(true)
		}

		sm := &c.Accounts[i].SMTP
		if sm.Port == 0 {
			sm.Port = DefaultSMTPPort
		}
		if sm.UseTLS == nil {
			sm.UseTLS = boolPtr(true)
		}
		if sm.Host == "" {
			sm.Host = is.Host
		}
		if sm.Username == "" {
			sm.Username = is.Username
		}
		if sm.Password == "" && sm.PasswordCommand == "" {
			sm.Password = is.Password
			sm.PasswordCommand = is.PasswordCommand
		}
	}
}

// validate reports every rule the config must satisfy.
func (c *Config) validate() error {
	if len(c.Accounts) == 0 {
		return errors.New("config: at least one [[accounts]] entry is required")
	}

	switch c.General.ReplyPosition {
	case ReplyPositionTop, ReplyPositionBottom:
	default:
		return fmt.Errorf("config: general.reply_position is %q, want %q or %q",
			c.General.ReplyPosition, ReplyPositionTop, ReplyPositionBottom)
	}

	if err := c.Layout.validate(); err != nil {
		return err
	}

	seen := make(map[string]bool, len(c.Accounts))
	defaults := 0

	for i := range c.Accounts {
		a := &c.Accounts[i]
		label := fmt.Sprintf("accounts[%d]", i)

		if a.Name == "" {
			return fmt.Errorf("config: %s.name is required", label)
		}
		if seen[a.Name] {
			return fmt.Errorf("config: duplicate account name %q", a.Name)
		}
		seen[a.Name] = true

		if a.IMAP.Host == "" {
			return fmt.Errorf("config: account %q: imap.host is required", a.Name)
		}
		if a.IMAP.Username == "" {
			return fmt.Errorf("config: account %q: imap.username is required", a.Name)
		}
		if err := checkCredential("imap", a.Name, a.IMAP.Password, a.IMAP.PasswordCommand); err != nil {
			return err
		}
		if err := checkCredential("smtp", a.Name, a.SMTP.Password, a.SMTP.PasswordCommand); err != nil {
			return err
		}

		if a.Default {
			defaults++
		}
	}

	if defaults > 1 {
		return errors.New("config: only one account may set default = true")
	}

	return nil
}

// checkCredential requires exactly one credential source.
func checkCredential(section, account, password, command string) error {
	hasPassword := password != ""
	hasCommand := command != ""

	if hasPassword && hasCommand {
		return fmt.Errorf("config: account %q: %s.password and %s.password_command are mutually exclusive",
			account, section, section)
	}
	if !hasPassword && !hasCommand {
		return fmt.Errorf("config: account %q: %s.password or %s.password_command is required",
			account, section, section)
	}

	return nil
}

// Default returns the account to open first. An account marked default wins;
// otherwise the first account does.
func (c *Config) Default() (*Account, error) {
	if len(c.Accounts) == 0 {
		return nil, errors.New("config: no accounts")
	}

	for i := range c.Accounts {
		if c.Accounts[i].Default {
			return &c.Accounts[i], nil
		}
	}

	return &c.Accounts[0], nil
}

// Account looks up an account by name.
func (c *Config) Account(name string) (*Account, error) {
	for i := range c.Accounts {
		if c.Accounts[i].Name == name {
			return &c.Accounts[i], nil
		}
	}

	return nil, fmt.Errorf("config: no account named %q", name)
}

// Address returns host:port for the IMAP server.
func (a *Account) IMAPAddress() string {
	return fmt.Sprintf("%s:%d", a.IMAP.Host, a.IMAP.Port)
}

// Address returns host:port for the SMTP server.
func (a *Account) SMTPAddress() string {
	return fmt.Sprintf("%s:%d", a.SMTP.Host, a.SMTP.Port)
}

// Sender returns the From address: the configured one, or the login name.
func (a *Account) Sender() string {
	if a.From != "" {
		return a.From
	}

	return a.IMAP.Username
}

// StartFolderName resolves start_folder: INBOX when unset, "" when disabled.
func (a *Account) StartFolderName() string {
	if a.StartFolder == nil {
		return "INBOX"
	}

	return strings.TrimSpace(*a.StartFolder)
}

func boolPtr(v bool) *bool {
	return &v
}
