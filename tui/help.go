package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// helpKeyColumn is the width of the key column. Keys are user input from [keys], so a
// hostile one has to be truncated rather than allowed to widen the dialog; 14 fits the
// longest shipped key (ctrl+shift+r is 12) with room to spare.
const helpKeyColumn = 14

// helpChrome is the rows the overlay spends outside the scrollable body: the title, the
// hint line, and ModalStyle's border and padding. Keeping it minimal buys a row for the
// keymap at the 60x20 floor.
const helpChrome = 6

// helpRow is one line of the help overlay: one or two bound actions, whose keys come
// from the resolved map, or a literal key that never goes through the keymap.
type helpRow struct {
	actions []action
	key     string
	label   string
}

// helpAction documents one bound action.
func helpAction(act action, label string) helpRow {
	return helpRow{actions: []action{act}, label: label}
}

// helpPair documents two actions shown together, such as up/down.
func helpPair(first, second action, label string) helpRow {
	return helpRow{actions: []action{first, second}, label: label}
}

// helpLiteral documents a key the composer reads itself. It is listed so help covers
// the whole keyboard, but it is not a rebindable action.
func helpLiteral(key, label string) helpRow {
	return helpRow{key: key, label: label}
}

// helpGroup is one heading and the rows under it.
type helpGroup struct {
	title string
	rows  []helpRow
}

// helpGroups is the whole keymap, grouped by the context a key works in. An action may
// appear more than once when it works in more than one place — delete acts on the list
// and in the reader — because the groups describe contexts, not the action table.
func helpGroups() []helpGroup {
	return []helpGroup{
		{
			title: "global",
			rows: []helpRow{
				helpAction(actionQuit, "quit"),
				helpAction(actionForceQuit, "quit now"),
				helpAction(actionNextPanel, "next panel"),
				helpAction(actionPrevPanel, "previous panel"),
				helpAction(actionReload, "reload folders"),
				helpAction(actionAccount, "accounts"),
				helpPair(actionAccountPrev, actionAccountNext, "previous/next account"),
				helpAction(actionFoldersOverlay, "folders overlay"),
				helpAction(actionHelp, "this help"),
			},
		},
		{
			title: "list",
			rows: []helpRow{
				helpPair(actionUp, actionDown, "move"),
				helpPair(actionTop, actionBottom, "first/last"),
				helpAction(actionOpen, "open"),
				helpAction(actionFilter, "filter"),
				helpAction(actionVisualRange, "select range"),
				helpAction(actionDelete, "delete"),
				helpAction(actionArchive, "archive"),
				helpAction(actionJunk, "junk"),
				helpAction(actionMove, "move to folder"),
				helpAction(actionEmptyTrash, "empty Trash"),
				helpAction(actionCollapse, "collapse thread"),
				helpAction(actionToggleFlag, "flag"),
				helpAction(actionToggleRead, "read/unread"),
				helpAction(actionAttachments, "attachments"),
			},
		},
		{
			title: "reader",
			rows: []helpRow{
				helpPair(actionUp, actionDown, "scroll"),
				helpAction(actionDelete, "delete"),
				helpAction(actionArchive, "archive"),
				helpAction(actionJunk, "junk"),
				helpAction(actionMove, "move to folder"),
				helpAction(actionToggleFlag, "flag"),
				helpAction(actionToggleRead, "read/unread"),
				helpAction(actionNextThreadMessage, "next in thread"),
				helpAction(actionPrevThreadMessage, "previous in thread"),
				helpAction(actionAttachments, "attachments"),
				helpAction(actionCompose, "compose"),
				helpAction(actionReply, "reply"),
				helpAction(actionReplyAll, "reply all"),
				helpAction(actionForward, "forward"),
				helpAction(actionBack, "back"),
			},
		},
		{
			title: "composer",
			rows: []helpRow{
				helpLiteral("ctrl+s", "save draft"),
				helpLiteral("ctrl+d", "send"),
				helpLiteral("tab", "next field"),
				helpLiteral("shift+tab", "previous field"),
				helpLiteral("esc", "cancel"),
			},
		},
		{
			title: "modals",
			rows: []helpRow{
				helpPair(actionUp, actionDown, "choose"),
				helpAction(actionOpen, "confirm"),
				helpAction(actionBack, "cancel"),
				helpAction(actionQuit, "close"),
			},
		},
	}
}

// helpLine is one rendered body line. Headings are styled at render time, after
// truncation, so a user-supplied key can never leave half an escape sequence behind.
type helpLine struct {
	text    string
	heading bool
}

// helpKeys is the key text for a row, read from the resolved map so a rebound key is
// what help shows. A row whose action has no keys renders nothing and is skipped.
func helpKeys(b bindings, row helpRow) string {
	if len(row.actions) == 0 {
		return row.key
	}

	if len(row.actions) == 1 {
		return strings.Join(b[row.actions[0]], ", ")
	}

	parts := make([]string, 0, len(row.actions))
	for _, act := range row.actions {
		key := primaryKey(b, act)
		if key == "" {
			return ""
		}

		parts = append(parts, key)
	}

	return strings.Join(parts, "/")
}

// helpLines renders the body: a heading per group and one line per documented row. The
// count feeds the scroll clamp, so it must not depend on the terminal width.
func (m Model) helpLines() []helpLine {
	lines := make([]helpLine, 0, len(actionNames)+len(helpGroups()))

	for _, group := range helpGroups() {
		lines = append(lines, helpLine{text: group.title, heading: true})

		for _, row := range group.rows {
			keys := helpKeys(m.keys, row)
			if keys == "" {
				continue
			}

			keys = TruncateDisplay(keys, helpKeyColumn)

			lines = append(lines, helpLine{
				text: keys + strings.Repeat(" ", helpKeyColumn-lipgloss.Width(keys)) + "  " + row.label,
			})
		}
	}

	return lines
}

// helpBodyHeight is how many body lines fit at the current terminal size.
func (m Model) helpBodyHeight() int {
	return max(1, ContentHeight(m.height)-helpChrome)
}

// helpMaxScroll is the last scroll offset that still fills the body.
func (m Model) helpMaxScroll() int {
	return max(0, len(m.helpLines())-m.helpBodyHeight())
}

// clampHelpScroll keeps the offset inside the list after a resize or a jump.
func (m *Model) clampHelpScroll() {
	m.helpScroll = clamp(m.helpScroll, 0, m.helpMaxScroll())
}

// handleHelpKey applies a keypress while the help overlay is open. It runs before every
// other context, so closing help cannot also trigger the main view's "back".
func (m Model) handleHelpKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.keys.resolve(msg.String()) {
	case actionForceQuit:
		return m, tea.Quit

	case actionHelp, actionBack, actionQuit:
		m.helpOpen = false
		m.helpScroll = 0

	case actionUp:
		m.helpScroll = max(0, m.helpScroll-1)

	case actionDown:
		m.helpScroll = min(m.helpScroll+1, m.helpMaxScroll())

	case actionTop:
		m.helpScroll = 0

	case actionBottom:
		m.helpScroll = m.helpMaxScroll()
	}

	return m, nil
}

// renderHelpOverlay centres the keymap in the content area, the same shape as the
// folders overlay. The body scrolls when it does not fit, which is what keeps the
// overlay usable at 60x20.
func (m Model) renderHelpOverlay(width, height int) string {
	inner := max(1, width-6)
	lines := m.helpLines()
	body := max(1, min(height-helpChrome, len(lines)))

	start := clamp(m.helpScroll, 0, max(0, len(lines)-body))
	end := min(start+body, len(lines))

	out := []string{TitleStyle.Render("keys")}
	for _, line := range lines[start:end] {
		text := TruncateDisplay(line.text, inner)
		if line.heading {
			text = MutedStyle.Render(text)
		}

		out = append(out, text)
	}

	out = append(out, MutedStyle.Render(TruncateDisplay(m.helpHint(), inner)))

	modal := ModalStyle.Render(strings.Join(out, "\n"))

	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, modal)
}

// helpHint renders the overlay's own keys from the binding table, so a rebound esc or
// arrow is advertised correctly inside the overlay too.
func (m Model) helpHint() string {
	hints := []string{
		hintText(m.keys, hint(actionBack, "close")),
		hintText(m.keys, hintPair(actionUp, actionDown, "scroll")),
	}

	parts := make([]string, 0, len(hints))
	for _, text := range hints {
		if text != "" {
			parts = append(parts, text)
		}
	}

	return strings.Join(parts, "  ")
}
