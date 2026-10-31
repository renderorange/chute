package tui

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/emersion/go-imap/v2"
	"github.com/renderorange/chute/mail"
)

// noVisualAnchor marks the cursor as the only selected row.
const noVisualAnchor = -1

// visualActive reports whether a range selection is in progress.
func (m Model) visualActive() bool {
	return m.visualAnchor != noVisualAnchor
}

// toggleVisual starts a range at the cursor, or clears the range already in
// progress. It only applies to the message list.
func (m *Model) toggleVisual() {
	if m.visualActive() {
		m.visualAnchor = noVisualAnchor
		return
	}
	if m.focus != focusMessages || len(m.rows) == 0 {
		return
	}

	m.visualAnchor = m.messageCursor
	m.status = ""
}

// visualBounds returns the first and last selected row, in that order, whether
// the cursor moved above or below the anchor.
func (m Model) visualBounds() (int, int) {
	start, end := m.visualAnchor, m.messageCursor
	if start > end {
		start, end = end, start
	}

	return clamp(start, 0, len(m.rows)-1), clamp(end, 0, len(m.rows)-1)
}

// inVisual reports whether a row falls inside the active range.
func (m Model) inVisual(index int) bool {
	if !m.visualActive() {
		return false
	}

	start, end := m.visualBounds()

	return index >= start && index <= end
}

// actionUIDs is the set every triage action applies to: the whole range when one
// is active, otherwise the message under the cursor.
func (m Model) actionUIDs() []imap.UID {
	if len(m.rows) == 0 {
		return nil
	}

	start, end := m.messageCursor, m.messageCursor
	if m.visualActive() {
		start, end = m.visualBounds()
	}

	uids := make([]imap.UID, 0, end-start+1)
	for i := start; i <= end; i++ {
		uids = append(uids, m.rows[i].Message.UID)
	}

	return uids
}

// toggleRead flips \Seen on the selection. A mixed selection is marked read,
// which is what mail clients do and what a user expects from one keypress.
func (m Model) toggleRead() (tea.Model, tea.Cmd) {
	if !m.triageReady() {
		return m, nil
	}

	uids := m.actionUIDs()
	if len(uids) == 0 {
		m.status = "no message selected"
		return m, nil
	}

	return m.storeFlagsCmd(uids, !m.allSeen(uids), false, imap.FlagSeen)
}

// toggleFlag flips \Flagged on the selection, using the same mixed-selection
// rule as toggleRead.
func (m Model) toggleFlag() (tea.Model, tea.Cmd) {
	if !m.triageReady() {
		return m, nil
	}

	uids := m.actionUIDs()
	if len(uids) == 0 {
		m.status = "no message selected"
		return m, nil
	}

	return m.storeFlagsCmd(uids, !m.allFlagged(uids), false, imap.FlagFlagged)
}

// allSeen reports whether every listed message is already read.
func (m Model) allSeen(uids []imap.UID) bool {
	return m.allHave(uids, func(msg mail.Message) bool { return msg.Seen() })
}

// allFlagged reports whether every listed message is already flagged.
func (m Model) allFlagged(uids []imap.UID) bool {
	return m.allHave(uids, func(msg mail.Message) bool { return msg.Flagged() })
}

// allHave reports whether every listed message satisfies have.
func (m Model) allHave(uids []imap.UID, have func(mail.Message) bool) bool {
	wanted := make(map[imap.UID]bool, len(uids))
	for _, uid := range uids {
		wanted[uid] = true
	}

	checked := make(map[imap.UID]bool, len(uids))
	for _, row := range m.rows {
		msg := row.Message
		if !wanted[msg.UID] {
			continue
		}
		checked[msg.UID] = true
		if !have(msg) {
			return false
		}
	}

	return len(checked) == len(wanted)
}

// storeFlagsCmd sends one flag change for the whole selection. quiet suppresses
// the error in the status bar, for changes the user did not ask for directly.
func (m Model) storeFlagsCmd(uids []imap.UID, add bool, quiet bool, flag imap.Flag) (tea.Model, tea.Cmd) {
	if m.storeFlags == nil || m.openFolder == "" || len(m.cfg.Accounts) == 0 {
		return m, nil
	}

	acct := m.cfg.Accounts[m.account]
	folder := m.openFolder
	store := m.storeFlags
	m.status = ""

	return m, func() tea.Msg {
		err := store(acct, folder, uids, add, flag)

		return flagsMsg{
			account: acct.Name,
			folder:  folder,
			uids:    uids,
			add:     add,
			flags:   []imap.Flag{flag},
			quiet:   quiet,
			err:     err,
		}
	}
}

// markSeenCmd marks one opened message read. Fetching a body never sets \Seen;
// opening the message deliberately does. A failure is quiet: the user did not
// ask for this, and a read-only folder would otherwise report an error on every
// open.
func (m Model) markSeenCmd(uid imap.UID, alreadySeen bool) tea.Cmd {
	if alreadySeen {
		return nil
	}

	_, cmd := m.storeFlagsCmd([]imap.UID{uid}, true, true, imap.FlagSeen)

	return cmd
}

// applyFlags updates the local rows the server confirmed, so the view matches the
// server without a refetch. A failed command changes nothing.
func (m *Model) applyFlags(uids []imap.UID, flags []imap.Flag, on bool) {
	wanted := make(map[imap.UID]bool, len(uids))
	for _, uid := range uids {
		wanted[uid] = true
	}

	for i := range m.messages {
		if !wanted[m.messages[i].UID] {
			continue
		}
		for _, flag := range flags {
			m.messages[i].SetFlag(flag, on)
		}
	}

	// Rows carry a copy of each message, so re-derive them or the list would keep
	// showing the old flag state.
	m.rebuildRows()
}
