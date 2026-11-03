package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/renderorange/chute/config"
	"github.com/renderorange/chute/mail"
)

// openSettings shows the read-only settings report.
func (m Model) openSettings() (tea.Model, tea.Cmd) {
	m.settingsOpen = true
	m.settingsScroll = 0
	m.status = ""

	return m, nil
}

// handleSettingsKey scrolls the report and closes it.
func (m Model) handleSettingsKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.keys.resolve(msg.String()) {
	case actionForceQuit:
		return m, tea.Quit

	case actionQuit, actionBack, actionSettings:
		m.settingsOpen = false

	case actionUp:
		m.settingsScroll = max(0, m.settingsScroll-1)

	case actionDown:
		m.settingsScroll++
	}

	return m, nil
}

// settingsLines is the report: effective account, roles, general, layout, list and build
// facts, with the source of every derived value. It reads Model and Config only, and it never
// touches a password or a password command (#67).
func (m Model) settingsLines() []string {
	lines := []string{"chute " + m.version, ""}

	if len(m.cfg.Accounts) > 0 {
		acct := m.cfg.Accounts[m.account]
		lines = append(lines, "account: "+acct.Name)

		from := acct.From
		if from == "" {
			from = "(imap.username)"
		}
		lines = append(lines, "  from: "+from)
		lines = append(lines, "  imap: "+acct.IMAPAddress()+" ("+imapTLS(acct)+")")
		lines = append(lines, "  smtp: "+acct.SMTPAddress()+" ("+smtpTLS(acct)+")")

		start := "INBOX (default)"
		if acct.StartFolder != nil {
			if *acct.StartFolder == "" {
				start = "(none)"
			} else {
				start = *acct.StartFolder
			}
		}
		lines = append(lines, "  start folder: "+start)

		hidden := "none"
		if len(acct.IMAP.HideFolders) > 0 {
			hidden = strings.Join(acct.IMAP.HideFolders, ", ")
		}
		lines = append(lines, "  hidden folders: "+hidden, "")

		lines = append(lines, "roles")
		for _, role := range []mail.Role{mail.RoleSent, mail.RoleDrafts, mail.RoleTrash, mail.RoleArchive, mail.RoleJunk} {
			name := "not found"
			if folder, ok := mail.ResolveFolder(m.folders, role, roleMailbox(acct, role)); ok {
				name = folder.Name
			}
			lines = append(lines, fmt.Sprintf("  %-8s %s", role, name))
		}
		lines = append(lines, "")
	}

	download := m.cfg.General.DownloadDir
	if download == "" {
		download = "~/Downloads (default)"
	}

	reply := m.cfg.General.ReplyPosition
	if reply == "" {
		reply = config.ReplyPositionTop + " (default)"
	}

	lines = append(lines,
		"general",
		fmt.Sprintf("  poll interval: %d s", m.cfg.General.PollInterval),
		"  download dir: "+download,
		"  reply position: "+reply,
		"",
	)

	layout := m.layout()
	lines = append(lines,
		"layout",
		fmt.Sprintf("  folders: %d (%s)", layout.Folders.Width, widthSource(m.cfg.Layout.FoldersWidth, m.cfg.Layout.FoldersPercent)),
		fmt.Sprintf("  messages: %d (%s)", layout.Messages.Width, widthSource(m.cfg.Layout.MessagesWidth, m.cfg.Layout.MessagesPercent)),
		fmt.Sprintf("  reader: %d (the rest)", layout.Reader.Width),
		"",
	)

	dateFormat := m.cfg.List.DateFormat
	if dateFormat == "" {
		dateFormat = config.ListDateFormat + " (default)"
	}
	lines = append(lines,
		"list",
		"  columns: "+strings.Join(m.cfg.List.ColumnOrder(), ", "),
		"  date format: "+dateFormat,
		"",
		"keys",
		fmt.Sprintf("  %d actions; press ? for the effective map", len(config.KeyActionNames())),
		"",
		"build",
		"  config: "+orUnknown(m.configPath),
		"  state: "+statePathText(),
		"  debug log: "+debugText(m.debugPath),
	)

	return lines
}

// roleMailbox returns the account's configured mailbox for a role, or "" when special-use
// detection decides.
func roleMailbox(acct config.Account, role mail.Role) string {
	switch role {
	case mail.RoleSent:
		return acct.IMAP.Sent
	case mail.RoleDrafts:
		return acct.IMAP.Drafts
	case mail.RoleTrash:
		return acct.IMAP.Trash
	case mail.RoleArchive:
		return acct.IMAP.Archive
	case mail.RoleJunk:
		return acct.IMAP.Junk
	default:
		return ""
	}
}

// imapTLS describes the IMAP transport in words, so a screenshot needs no decoding.
func imapTLS(acct config.Account) string {
	if acct.IMAP.UseTLS != nil && !*acct.IMAP.UseTLS {
		return "STARTTLS"
	}

	return "implicit TLS"
}

// smtpTLS describes the SMTP transport.
func smtpTLS(acct config.Account) string {
	if acct.SMTP.UseTLS != nil && !*acct.SMTP.UseTLS {
		return "STARTTLS when offered"
	}

	return "implicit TLS"
}

// widthSource says which rule produced a pane's width: the part a user cannot infer from the
// rendered screen.
func widthSource(width, percent int) string {
	switch {
	case width > 0:
		return "configured width"
	case percent > 0:
		return "configured percent"
	default:
		return "automatic"
	}
}

// orUnknown names an unset path.
func orUnknown(path string) string {
	if path == "" {
		return "(unknown)"
	}

	return path
}

// statePathText reports the selection file the app would write.
func statePathText() string {
	path, err := config.StatePath()
	if err != nil {
		return "(unknown)"
	}

	return path
}

// debugText reports the debug log, which is set by -debug and not by the config.
func debugText(path string) string {
	if path == "" {
		return "off (-debug to enable)"
	}

	return path
}

// renderSettingsModal centres the windowed report.
func (m Model) renderSettingsModal(width, height int) string {
	lines := m.settingsLines()

	// The modal spends eight rows outside the scrolled body: border and padding, the title,
	// the blank line under it, the blank line above the hint, and the hint itself.
	rows := max(1, height-8)
	start := clamp(m.settingsScroll, 0, max(0, len(lines)-rows))
	end := min(start+rows, len(lines))

	inner := max(1, width-6)

	body := make([]string, 0, rows+4)
	body = append(body, TitleStyle.Render("settings"), "")

	for _, line := range lines[start:end] {
		body = append(body, NormalStyle.Render(TruncateDisplay(line, inner)))
	}

	body = append(body, "", MutedStyle.Render("esc:close  up/down:scroll"))

	modal := ModalStyle.Render(strings.Join(body, "\n"))

	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, modal)
}
