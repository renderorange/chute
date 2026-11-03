package tui

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/emersion/go-imap/v2"
	"github.com/renderorange/chute/mail"
)

// schedulePoll arms the next new-mail check. A zero or negative interval disables
// polling rather than firing in a tight loop.
func (m Model) schedulePoll() tea.Cmd {
	seconds := m.cfg.General.PollInterval
	if seconds <= 0 || m.clock == nil {
		return nil
	}

	return m.clock(time.Duration(seconds)*time.Second, func(time.Time) tea.Msg {
		return pollTickMsg{}
	})
}

// pollCmd checks the open folder for new mail.
//
// It does nothing when no folder is open, when a check is already in flight, or
// while a dialog or the composer owns the keyboard: a list that reorders under the
// cursor while the user answers a delete confirmation is a way to act on the wrong
// message.
func (m Model) pollCmd() tea.Cmd {
	if m.polling || m.openFolder == "" || len(m.cfg.Accounts) == 0 {
		return nil
	}
	if m.accountsOpen || m.confirmOpen || m.attachOpen || m.foldersOpen || m.compose != nil {
		return nil
	}

	load := m.pollFolder
	if load == nil {
		return nil
	}

	acct := m.cfg.Accounts[m.account]
	folder := m.openFolder
	since := m.lastSeen[folderKey{account: acct.Name, folder: folder}]

	return func() tea.Msg {
		result, err := load(acct, folder, since)

		return pollMsg{account: acct.Name, folder: folder, result: result, err: err}
	}
}

// applyPoll folds one check into the list and reports whether another command is
// needed. A check that found fewer messages than are shown means another client
// removed some, so the caller then asks which survived.
func (m *Model) applyPoll(msg pollMsg) tea.Cmd {
	m.polling = false

	if msg.account != m.accountName() || msg.folder != m.openFolder {
		return nil
	}
	if msg.err != nil {
		// A failed poll is silent in the list and visible in the status bar. The
		// next tick recovers, and no dialog interrupts what the user is doing.
		m.status = statusText(msg.err)

		return nil
	}

	m.openUnseen = msg.result.Unseen
	m.mergeMessages(msg.result.Messages)
	m.advanceLastSeen(folderKey{account: msg.account, folder: msg.folder})

	if int(msg.result.Total) < len(m.messages) {
		return m.reconcileCmd(msg.account, msg.folder)
	}

	return nil
}

// mergeMessages folds polled envelopes into the list: a known UID takes the
// server's flags, an unknown one is added. Rows are rebuilt so a new reply lands
// inside its thread rather than at the end.
func (m *Model) mergeMessages(incoming []mail.Message) {
	if len(incoming) == 0 {
		return
	}

	index := make(map[imap.UID]int, len(m.messages))
	for i := range m.messages {
		index[m.messages[i].UID] = i
	}

	for _, msg := range incoming {
		if i, ok := index[msg.UID]; ok {
			m.messages[i].Flags = msg.Flags
			continue
		}
		m.messages = append(m.messages, msg)
	}

	m.messages = sortNewestFirst(m.messages)
	m.rebuildRows()
}

// advanceLastSeen raises the high-water mark for a folder to the highest UID now
// known. It never lowers it: a message the user deleted stays the mark, so a poll
// does not fetch it again.
func (m *Model) advanceLastSeen(key folderKey) {
	highest := m.lastSeen[key]
	for i := range m.messages {
		if m.messages[i].UID > highest {
			highest = m.messages[i].UID
		}
	}

	if m.lastSeen == nil {
		m.lastSeen = make(map[folderKey]imap.UID)
	}
	m.lastSeen[key] = highest
}

// reconcileCmd asks which UIDs survived a deletion another client made.
func (m Model) reconcileCmd(account, folder string) tea.Cmd {
	load := m.countUIDs
	if load == nil || len(m.cfg.Accounts) == 0 {
		return nil
	}

	acct := m.cfg.Accounts[m.account]

	return func() tea.Msg {
		uids, err := load(acct, folder)

		return uidsMsg{account: account, folder: folder, uids: uids, err: err}
	}
}

// applyUIDs drops the rows another client removed and leaves the cursor on a
// neighbour, the same discipline as a local delete.
func (m *Model) applyUIDs(msg uidsMsg) {
	if msg.err != nil {
		m.status = statusText(msg.err)

		return
	}
	if msg.account != m.accountName() || msg.folder != m.openFolder {
		return
	}

	alive := make(map[imap.UID]bool, len(msg.uids))
	for _, uid := range msg.uids {
		alive[uid] = true
	}

	kept := m.messages[:0]
	for _, message := range m.messages {
		if alive[message.UID] {
			kept = append(kept, message)
		}
	}
	m.messages = kept
	m.rebuildRows()
	m.visualAnchor = noVisualAnchor
}
