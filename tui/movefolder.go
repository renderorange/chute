package tui

import (
	"errors"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/emersion/go-imap/v2"
	"github.com/renderorange/chute/mail"
	"github.com/sahilm/fuzzy"
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
	m.moveFilter = ""
	m.moveMatches = folders
	m.status = ""

	return m, nil
}

// matchFolders narrows the destination list to the folders matching query, in
// folder order. The match is fuzzy and case-insensitive, the same rules the list
// filter uses (#95), so "pr" finds "Projects" and "ach" finds "Archive".
func matchFolders(folders []mail.Folder, query string) []mail.Folder {
	query = strings.TrimSpace(query)
	if query == "" {
		return folders
	}

	names := make([]string, len(folders))
	for i, folder := range folders {
		names[i] = folder.Name
	}

	matched := make([]mail.Folder, 0, len(folders))
	for _, hit := range fuzzy.FindNoSort(query, names) {
		matched = append(matched, folders[hit.Index])
	}

	return matched
}

// applyMoveFilter re-derives the visible destinations after the query changed and
// puts the cursor back at the top, because the row it pointed at may be gone.
func (m *Model) applyMoveFilter() {
	m.moveMatches = matchFolders(m.moveFolders, m.moveFilter)
	m.moveCursor = 0
	m.status = ""
}

// closeMove dismisses the picker and forgets the query, so reopening it starts
// from the whole list.
func (m *Model) closeMove() {
	m.moveOpen = false
	m.moveFilter = ""
	m.moveMatches = nil
}

// handleMoveKey applies a keypress while the move picker is open. Printable keys
// belong to the filter query, as they do in the filter prompt, so a folder name
// can be typed; esc, enter, backspace and the arrows are the control keys.
// Rebound modal keys do not apply here, which is the same trade the filter and
// search prompts make.
func (m Model) handleMoveKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyCtrlC:
		return m, tea.Quit

	case tea.KeyEsc:
		m.closeMove()

	case tea.KeyEnter:
		return m.moveSelectionToFolder()

	case tea.KeyUp:
		m.moveCursor = max(0, m.moveCursor-1)

	case tea.KeyDown:
		m.moveCursor = max(0, min(m.moveCursor+1, len(m.moveMatches)-1))

	case tea.KeyBackspace:
		if runes := []rune(m.moveFilter); len(runes) > 0 {
			m.moveFilter = string(runes[:len(runes)-1])
			m.applyMoveFilter()
		}

	case tea.KeySpace:
		m.moveFilter += " "
		m.applyMoveFilter()

	case tea.KeyRunes:
		m.moveFilter += string(msg.Runes)
		m.applyMoveFilter()
	}

	return m, nil
}

// moveSelectionToFolder starts the move to the mailbox under the picker cursor.
func (m Model) moveSelectionToFolder() (tea.Model, tea.Cmd) {
	if !m.moveOpen {
		return m, nil
	}
	if len(m.moveMatches) == 0 {
		m.status = "no folders match"

		return m, nil
	}
	if m.moveCursor < 0 || m.moveCursor >= len(m.moveMatches) {
		m.closeMove()

		return m, nil
	}

	dest := m.moveMatches[m.moveCursor].Name
	m.closeMove()

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

// renderMoveModal centres the folder picker in the content area. The query line
// appears only once something is typed, so the default dialog keeps its rows.
func (m Model) renderMoveModal(width, height int) string {
	inner := max(10, width-14)

	chrome := 6
	lines := []string{TitleStyle.Render("move to folder")}
	if m.moveFilter != "" {
		chrome++
		lines = append(lines, NormalStyle.Render(TruncateDisplay(m.moveFilter+"_", inner)))
	}

	switch {
	case len(m.moveMatches) == 0:
		lines = append(lines, MutedStyle.Render("no folders match"))
	default:
		rows := max(1, min(len(m.moveMatches), height-chrome))
		start := clamp(m.moveCursor-rows/2, 0, max(0, len(m.moveMatches)-rows))
		end := min(start+rows, len(m.moveMatches))

		for i := start; i < end; i++ {
			lines = append(lines, folderRow(m.moveMatches[i], i == m.moveCursor, inner))
		}
	}

	lines = append(lines, "", MutedStyle.Render("enter:move  esc:close"))

	modal := ModalStyle.Render(strings.Join(lines, "\n"))

	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, modal)
}
