package config

import (
	"strings"
	"testing"
)

// account returns an account with the given credential settings.
func account(name, password, command string) Account {
	return Account{
		Name: name,
		IMAP: IMAP{Host: "imap.example.com", Username: "u", Password: password, PasswordCommand: command},
	}
}

func TestIMAPPasswordPlaintext(t *testing.T) {
	a := account("a", "hunter2", "")

	got, err := a.IMAPPassword()
	if err != nil {
		t.Fatalf("IMAPPassword returned error: %v", err)
	}
	if got != "hunter2" {
		t.Errorf("password = %q, want hunter2", got)
	}
}

func TestIMAPPasswordCommand(t *testing.T) {
	a := account("a", "", "printf 's3cret\\n'")

	got, err := a.IMAPPassword()
	if err != nil {
		t.Fatalf("IMAPPassword returned error: %v", err)
	}
	if got != "s3cret" {
		t.Errorf("password = %q, want s3cret with the newline trimmed", got)
	}
}

func TestIMAPPasswordCommandFailure(t *testing.T) {
	a := account("a", "", "exit 3")

	_, err := a.IMAPPassword()
	if err == nil {
		t.Fatal("IMAPPassword accepted a failing command")
	}
	if !strings.Contains(err.Error(), "password_command failed") {
		t.Errorf("error = %q, want it to name the failing command", err)
	}
}

func TestIMAPPasswordCommandEmpty(t *testing.T) {
	a := account("a", "", "printf ''")

	_, err := a.IMAPPassword()
	if err == nil {
		t.Fatal("IMAPPassword accepted empty output")
	}
	if !strings.Contains(err.Error(), "no output") {
		t.Errorf("error = %q, want it to report empty output", err)
	}
}

func TestSMTPPasswordInheritsIMAPCommand(t *testing.T) {
	a := Account{
		Name: "a",
		IMAP: IMAP{Host: "imap.example.com", Username: "u", PasswordCommand: "printf 'inherited'"},
	}
	a.SMTP.PasswordCommand = a.IMAP.PasswordCommand

	got, err := a.SMTPPassword()
	if err != nil {
		t.Fatalf("SMTPPassword returned error: %v", err)
	}
	if got != "inherited" {
		t.Errorf("password = %q, want inherited", got)
	}
}
