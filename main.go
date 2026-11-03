// Command chute is a terminal mail client.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/renderorange/chute/config"
	"github.com/renderorange/chute/mail"
	"github.com/renderorange/chute/tui"
)

// version is set at build time with -ldflags "-X main.version=...".
var version = "dev"

func main() {
	configPath := flag.String("config", defaultConfigPath(), "path to the config file")
	debugPath := flag.String("debug", "", "write the IMAP protocol log to this file, with the credential masked")
	showVersion := flag.Bool("version", false, "print the version and exit")
	check := flag.Bool("check", false, "connect to every account, list folders, check SMTP, then exit")
	flag.Parse()

	if *showVersion {
		fmt.Println("chute " + version)
		return
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	mailOptions, closeLog, err := debugOptions(*debugPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "chute: -debug: %v\n", err)
		os.Exit(1)
	}

	if *check {
		ok := runCheck(cfg, os.Stdout, os.Stderr, mailOptions...)
		closeLog()
		if !ok {
			os.Exit(1)
		}

		return
	}

	model := tui.New(cfg, version, mailOptions...).
		WithSelection(config.LoadState(), config.SaveState)
	program := tea.NewProgram(model, tea.WithAltScreen())

	_, runErr := program.Run()
	closeErr := model.Close()
	closeLog()

	if runErr != nil {
		fmt.Fprintln(os.Stderr, runErr)
		os.Exit(1)
	}
	if closeErr != nil {
		fmt.Fprintln(os.Stderr, closeErr)
	}
}

// debugOptions opens the protocol log when -debug was given. It returns the mail
// options that send every account's stream there, and a function that closes the
// file; repeated calls are safe and a missing path is a no-op.
//
// The file is truncated and created 0600: it is evidence for one run, it carries the
// session's mail traffic, and an earlier run's MOVE lines must not be read as this
// run's. An existing file keeps its own permissions.
func debugOptions(path string) ([]mail.Option, func(), error) {
	if path == "" {
		return nil, func() {}, nil
	}

	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return nil, nil, err
	}

	closed := false
	closeLog := func() {
		if closed {
			return
		}
		closed = true
		file.Close()
	}

	return []mail.Option{mail.WithDebugWriter(file)}, closeLog, nil
}

// runCheck verifies every account twice: IMAP (connect and list folders) and SMTP (connect,
// negotiate TLS, authenticate, then quit without sending). It prints the credential source
// but never the credential itself, and reports false when either half fails for any
// account.
func runCheck(cfg *config.Config, stdout, stderr io.Writer, mailOptions ...mail.Option) bool {
	ok := true

	for i := range cfg.Accounts {
		acct := &cfg.Accounts[i]
		ok = checkIMAP(acct, stdout, stderr, mailOptions...) && ok
		ok = checkSMTP(acct, stdout, stderr) && ok
	}

	return ok
}

// checkIMAP connects, lists folders and prints them. It is the half -check had first.
func checkIMAP(acct *config.Account, stdout, stderr io.Writer, mailOptions ...mail.Option) bool {
	source := "plaintext"
	if acct.IMAP.Password == "" {
		source = "command"
	}

	password, err := acct.IMAPPassword()
	if err != nil {
		fmt.Fprintf(stderr, "%s: %v\n", acct.Name, err)
		return false
	}

	client, err := mail.Connect(*acct, password, mailOptions...)
	if err != nil {
		fmt.Fprintf(stderr, "%s: %v\n", acct.Name, err)
		return false
	}

	folders, listErr := client.Folders()
	closeErr := client.Close()

	if listErr != nil {
		fmt.Fprintf(stderr, "%s: %v\n", acct.Name, listErr)
		return false
	}
	folders = mail.FilterFolders(folders, acct.IMAP.HideFolders)

	ok := true
	if closeErr != nil {
		fmt.Fprintf(stderr, "%s: logout: %v\n", acct.Name, closeErr)
		ok = false
	}

	tlsMode := "implicit"
	if acct.IMAP.UseTLS != nil && !*acct.IMAP.UseTLS {
		tlsMode = "starttls"
	}

	fmt.Fprintf(stdout, "%s: %s as %s, credential=%s, tls=%s, folders=%d\n",
		acct.Name, acct.IMAPAddress(), acct.IMAP.Username, source, tlsMode, len(folders))

	for _, folder := range folders {
		role := folder.Role()
		if role == mail.RoleNone {
			continue
		}
		fmt.Fprintf(stdout, "  %-8s %s\n", role, folder.Name)
	}

	return ok
}

// checkSMTP proves the outgoing half without sending anything. A send failure is the one
// the user cannot easily reason about afterwards, so it is worth catching before composing
// (#66).
func checkSMTP(acct *config.Account, stdout, stderr io.Writer) bool {
	source := "plaintext"
	if acct.SMTP.Password == "" {
		source = "command"
	}

	password, err := acct.SMTPPassword()
	if err != nil {
		fmt.Fprintf(stderr, "%s: smtp: %v\n", acct.Name, err)
		return false
	}

	if err := mail.CheckSMTP(*acct, password); err != nil {
		fmt.Fprintf(stderr, "%s: smtp %s: %v\n", acct.Name, acct.SMTPAddress(), err)
		return false
	}

	tlsMode := "implicit"
	if acct.SMTP.UseTLS != nil && !*acct.SMTP.UseTLS {
		tlsMode = "starttls"
	}

	// The host fallback is invisible in the config file, so say which rule produced it.
	hostSource := "configured"
	if acct.SMTP.HostInherited() {
		hostSource = "inherited"
	}

	fmt.Fprintf(stdout, "%s: smtp %s as %s, credential=%s, tls=%s, host=%s\n",
		acct.Name, acct.SMTPAddress(), acct.SMTP.Username, source, tlsMode, hostSource)

	return true
}

// defaultConfigPath is $XDG_CONFIG_HOME/chute/config.toml.
func defaultConfigPath() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "config.toml"
	}

	return filepath.Join(dir, "chute", "config.toml")
}
