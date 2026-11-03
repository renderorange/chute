package tui

import (
	"errors"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/emersion/go-imap/v2"
	"github.com/renderorange/chute/mail"
)

// folderMoveMsg reports a named move to an arbitrary mailbox.
type folderMoveMsg struct {
	account string
	folder  string
	dest    string
	uids    []imap.UID
	err     error
}

// beginMove opens the picker over every selectable mailbox except the one the messages
// are already in. The source is excluded rather than shown and refused: a destination
// that cannot work is noise in a list the user is choosing from.
func (m Model) beginMove() (tea.Model, tea.Cmd) {
	if !m.triageReady() {
		return m, nil
	}
	if next, blocked := m.resultsOnly(); blocked {
		return next, nil
	}

	if len(m.actionUIDs()) == 0 {
		m.status = "no message selected"
		return m, nil
	}

	folders := make([]mail.Folder, 0, len(m.folders))
	for _, folder := range m.folders {
		if !folder.Selectable() || strings.EqualFold(folder.Name, m.openFolder) {
			continue
		}
		folders = append(folders, folder)
	}

	if len(folders) == 0 {
		m.status = "no other folders"
		return m, nil
	}

	m.moveOpen = true
	m.moveCursor = 0
	m.moveFolders = folders
	m.status = ""

	return m, nil
}

// handleMoveKey applies a keypress while the move picker is open.
func (m Model) handleMoveKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.keys.resolve(msg.String()) {
	case actionForceQuit:
		return m, tea.Quit

	case actionQuit, actionBack, actionMove:
		m.moveOpen = false

	case actionUp:
		m.moveCursor = max(0, m.moveCursor-1)

	case actionDown:
		m.moveCursor = min(m.moveCursor+1, len(m.moveFolders)-1)

	case actionOpen:
		return m.moveSelectionToFolder()
	}

	return m, nil
}

// moveSelectionToFolder starts the move to the mailbox under the picker cursor.
func (m Model) moveSelectionToFolder() (tea.Model, tea.Cmd) {
	if !m.moveOpen || len(m.moveFolders) == 0 {
		return m, nil
	}
	if m.moveCursor < 0 || m.moveCursor >= len(m.moveFolders) {
		m.moveOpen = false
		return m, nil
	}

	dest := m.moveFolders[m.moveCursor].Name
	m.moveOpen = false

	uids := m.actionUIDs()
	if len(uids) == 0 {
		m.status = "no message selected"
		return m, nil
	}

	if m.moveToFolder == nil || m.openFolder == "" || len(m.cfg.Accounts) == 0 {
		return m, nil
	}

	acct := m.cfg.Accounts[m.account]
	folder := m.openFolder
	move := m.moveToFolder
	m.status = ""

	return m, func() tea.Msg {
		err := move(acct, folder, dest, uids)

		return folderMoveMsg{account: acct.Name, folder: folder, dest: dest, uids: uids, err: err}
	}
}

// folderMoveStatus turns the typed refusals into short statuses. Anything else is shown
// as the server said it.
func folderMoveStatus(msg folderMoveMsg) string {
	if errors.Is(msg.err, mail.ErrAlreadyInRoleFolder) {
		if msg.dest != "" {
			return "already in " + msg.dest
		}

		return "already there"
	}

	return statusText(msg.err)
}

// afterMoveSuccess applies the shared post-move list update.
func (m Model) afterMoveSuccess(uids []imap.UID, dest string) (tea.Model, tea.Cmd) {
	return m.afterRemoval(uids, fmt.Sprintf("moved %s to %s", countLabel(len(uids), "message"), dest))
}

// afterRemoval drops the rows the server confirmed gone, follows the neighbour and then
// sets the confirmation, because opening the neighbour clears the status bar.
func (m Model) afterRemoval(uids []imap.UID, status string) (tea.Model, tea.Cmd) {
	m.removeMessages(uids)

	next, cmd := m.followRows()
	next.status = status

	return next, cmd
}

// renderMoveModal centres the folder picker in the content area.
func (m Model) renderMoveModal(width, height int) string {
	if len(m.moveFolders) == 0 {
		return ""
	}

	rows := max(1, min(len(m.moveFolders), height-6))
	start := clamp(m.moveCursor-rows/2, 0, max(0, len(m.moveFolders)-rows))
	end := min(start+rows, len(m.moveFolders))

	lines := []string{TitleStyle.Render("move to folder")}
	for i := start; i < end; i++ {
		lines = append(lines, folderRow(m.moveFolders[i], i == m.moveCursor, max(10, width-14)))
	}
	lines = append(lines, "", MutedStyle.Render("enter:move  esc:close"))

	modal := ModalStyle.Render(strings.Join(lines, "\n"))

	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, modal)
}
