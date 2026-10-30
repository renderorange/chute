package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/renderorange/chute/mail"
)

// attachmentSavedMsg reports the result of writing one attachment to disk.
type attachmentSavedMsg struct {
	path string
	err  error
}

// openAttachments shows the attachment picker for the selected message.
func (m Model) openAttachments() (tea.Model, tea.Cmd) {
	body, ok := m.bodyFor(m.selectedBodyKey())
	if !ok {
		m.status = "load the message body first with enter"
		return m, nil
	}
	if len(body.Attachments) == 0 {
		m.status = "this message has no attachments"
		return m, nil
	}

	m.attachOpen = true
	m.attachCursor = 0
	m.status = ""

	return m, nil
}

// handleAttachKey applies a keypress while the attachment picker is open.
func (m Model) handleAttachKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	body, ok := m.bodyFor(m.selectedBodyKey())
	if !ok {
		m.attachOpen = false
		return m, nil
	}

	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit

	case "esc", "q":
		m.attachOpen = false

	case "up", "k":
		m.attachCursor = max(0, m.attachCursor-1)

	case "down", "j":
		m.attachCursor = min(m.attachCursor+1, len(body.Attachments)-1)

	case "enter", "s":
		return m.saveSelectedAttachment(body)
	}

	return m, nil
}

// saveSelectedAttachment writes the attachment under the picker cursor.
func (m Model) saveSelectedAttachment(body *mail.Body) (tea.Model, tea.Cmd) {
	if m.attachCursor < 0 || m.attachCursor >= len(body.Attachments) {
		m.attachOpen = false
		return m, nil
	}

	m.attachOpen = false

	return m, m.saveAttachmentCmd(body.Attachments[m.attachCursor])
}

// saveAttachmentCmd writes one attachment in the background.
func (m Model) saveAttachmentCmd(attachment mail.Attachment) tea.Cmd {
	dir := m.cfg.General.DownloadPath()

	return func() tea.Msg {
		path, err := mail.SaveAttachment(dir, attachment)

		return attachmentSavedMsg{path: path, err: err}
	}
}

// renderAttachmentModal centres the picker in the content area.
func (m Model) renderAttachmentModal(width, height int) string {
	body, ok := m.bodyFor(m.selectedBodyKey())
	if !ok {
		return ""
	}

	lines := []string{TitleStyle.Render("save attachment")}
	for i, attachment := range body.Attachments {
		label := fmt.Sprintf("%s  (%s)", attachment.Filename, formatSize(len(attachment.Data)))
		label = Truncate(label, max(10, width-14))

		if i == m.attachCursor {
			lines = append(lines, SelectedStyle.Render("> "+label))
		} else {
			lines = append(lines, "  "+label)
		}
	}
	lines = append(lines, "", MutedStyle.Render("enter:save  esc:close"))

	modal := ModalStyle.Render(strings.Join(lines, "\n"))

	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, modal)
}
