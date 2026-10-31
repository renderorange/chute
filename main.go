// Command chute is a terminal mail client.
package main

import (
	"flag"
	"fmt"
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
	check := flag.Bool("check", false, "connect to every account, list folders, then exit")
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
		ok := runCheck(cfg, mailOptions...)
		closeLog()
		if !ok {
			os.Exit(1)
		}

		return
	}

	model := tui.New(cfg, version, mailOptions...)
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

// runCheck verifies every account can connect and list folders. It prints the
// credential source but never the credential itself. It reports success.
func runCheck(cfg *config.Config, mailOptions ...mail.Option) bool {
	ok := true

	for i := range cfg.Accounts {
		acct := &cfg.Accounts[i]
		source := "plaintext"
		if acct.IMAP.Password == "" {
			source = "command"
		}

		password, err := acct.IMAPPassword()
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: %v\n", acct.Name, err)
			ok = false
			continue
		}

		client, err := mail.Connect(*acct, password, mailOptions...)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: %v\n", acct.Name, err)
			ok = false
			continue
		}

		folders, listErr := client.Folders()
		closeErr := client.Close()

		if listErr != nil {
			fmt.Fprintf(os.Stderr, "%s: %v\n", acct.Name, listErr)
			ok = false
			continue
		}
		folders = mail.FilterFolders(folders, acct.IMAP.HideFolders)
		if closeErr != nil {
			fmt.Fprintf(os.Stderr, "%s: logout: %v\n", acct.Name, closeErr)
			ok = false
		}

		fmt.Printf("%s: %s as %s, credential=%s, folders=%d\n",
			acct.Name, acct.IMAPAddress(), acct.IMAP.Username, source, len(folders))

		for _, folder := range folders {
			role := folder.Role()
			if role == mail.RoleNone {
				continue
			}
			fmt.Printf("  %-8s %s\n", role, folder.Name)
		}
	}

	return ok
}

// defaultConfigPath is $XDG_CONFIG_HOME/chute/config.toml.
func defaultConfigPath() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "config.toml"
	}

	return filepath.Join(dir, "chute", "config.toml")
}
