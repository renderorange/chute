package config

import (
	"fmt"
	"os/exec"
	"strings"
)

// IMAPPassword returns the password for the account's IMAP server.
func (a *Account) IMAPPassword() (string, error) {
	return resolveCredential("imap", a.Name, a.IMAP.Password, a.IMAP.PasswordCommand)
}

// SMTPPassword returns the password for the account's SMTP server.
func (a *Account) SMTPPassword() (string, error) {
	return resolveCredential("smtp", a.Name, a.SMTP.Password, a.SMTP.PasswordCommand)
}

// resolveCredential returns the plaintext password, or runs the password command.
//
// The command runs through sh -c so it can pipe and quote. Its output is trimmed of
// surrounding whitespace, which is how pass and secret-tool terminate a secret.
func resolveCredential(section, account, password, command string) (string, error) {
	if password != "" {
		return password, nil
	}
	if command == "" {
		return "", fmt.Errorf("config: account %q: no %s credential configured", account, section)
	}

	out, err := exec.Command("sh", "-c", command).Output()
	if err != nil {
		return "", fmt.Errorf("config: account %q: %s password_command failed: %w", account, section, err)
	}

	secret := strings.TrimSpace(string(out))
	if secret == "" {
		return "", fmt.Errorf("config: account %q: %s password_command produced no output", account, section)
	}

	return secret, nil
}
