package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// hintSeparator sits between two hints. It is one cell, not two: at 105 columns
// the content width is 99, and the priority set plus back plus the compose keys
// costs 99 with single spaces and 109 with double ones. A second space would
// push a hint off the footer, which is the defect this file exists to fix.
const hintSeparator = " "

// footerHint is one "key:label" reminder. A hint is either derived from the
// binding table (actions) or literal, for the composer, whose keys are read
// outside the keymap.
type footerHint struct {
	actions []action
	literal string
	label   string
}

// hint builds a hint for one bound action.
func hint(act action, label string) footerHint {
	return footerHint{actions: []action{act}, label: label}
}

// hintPair builds a hint for two bound actions that share a label, such as the
// two movement keys, rendered as "up/down:move".
func hintPair(first, second action, label string) footerHint {
	return footerHint{actions: []action{first, second}, label: label}
}

// literalHint builds a hint for a key that is not in the binding table.
func literalHint(key, label string) footerHint {
	return footerHint{literal: key, label: label}
}

// primaryKey returns the key the footer should advertise for an action: the
// shortest key, so an action bound to both a letter and an arrow shows the letter
// and the footer keeps its cell budget. A key that is not a single cell ("esc",
// "up", "ctrl+r") is used as is.
func primaryKey(b bindings, act action) string {
	keys := b[act]

	for _, key := range keys {
		if lipgloss.Width(key) == 1 {
			return key
		}
	}

	if len(keys) > 0 {
		return keys[0]
	}

	return ""
}

// hintText renders one hint, or "" when it must not be shown: an action with no
// binding cannot be advertised, and a partially bound pair is dropped whole
// rather than naming half a gesture.
func hintText(b bindings, h footerHint) string {
	key := h.literal

	if len(h.actions) > 0 {
		keys := make([]string, 0, len(h.actions))
		for _, act := range h.actions {
			one := primaryKey(b, act)
			if one == "" {
				return ""
			}

			keys = append(keys, one)
		}

		key = strings.Join(keys, "/")
	}

	if key == "" || h.label == "" {
		return ""
	}

	return key + ":" + h.label
}

// hintKeyPart returns the key side of a rendered hint, so a test can resolve it
// back through the binding table.
func hintKeyPart(text string) string {
	key, _, _ := strings.Cut(text, ":")

	return key
}

// packHints joins the hints in priority order and drops whole hints from the end
// once the next one would not fit. Cutting a hint in half is what the old footer
// did; dropping one leaves every visible hint true and readable.
func packHints(b bindings, hints []footerHint, width int) string {
	var parts []string

	used := 0
	for _, h := range hints {
		text := hintText(b, h)
		if text == "" {
			continue
		}

		add := lipgloss.Width(text)
		if len(parts) > 0 {
			add += len(hintSeparator)
		}
		if used+add > width {
			break
		}

		parts = append(parts, text)
		used += add
	}

	return strings.Join(parts, hintSeparator)
}

// footerHints lists the hints for the current context, in priority order: the
// keys that prevent a mistake first (delete, flag, read, open, quit). The list is
// deliberately short — the footer is a reminder of the keys used most, not the
// keymap, and a wall of hints is unreadable. Everything else belongs in the help
// screen (#21). A context lists only the keys it honours, and the width budget in
// packHints is a safety net rather than an invitation to fill it.
func (m Model) footerHints() []footerHint {
	switch {
	case m.helpOpen:
		return []footerHint{hint(actionBack, "close"), hintPair(actionUp, actionDown, "scroll")}
	case m.filterOpen:
		return []footerHint{hint(actionOpen, "keep"), hint(actionBack, "clear")}
	case m.confirmOpen && m.confirmKind == confirmDiscardDraft:
		return []footerHint{hint(actionOpen, "discard"), hint(actionBack, "keep editing")}
	case m.confirmOpen:
		return []footerHint{hint(actionOpen, "delete"), hint(actionBack, "cancel")}
	case m.compose != nil:
		return []footerHint{
			literalHint("ctrl+s", "save"),
			literalHint("ctrl+d", "send"),
			literalHint("tab", "field"),
			literalHint("esc", "cancel"),
		}
	case m.accountsOpen:
		return []footerHint{
			hintPair(actionUp, actionDown, "choose"),
			hint(actionOpen, "switch"),
			hint(actionBack, "close"),
		}
	case m.attachOpen:
		return []footerHint{
			hintPair(actionUp, actionDown, "choose"),
			hint(actionOpen, "save"),
			hint(actionBack, "close"),
		}
	case m.foldersOpen:
		return []footerHint{
			hintPair(actionUp, actionDown, "choose"),
			hint(actionOpen, "open"),
			hint(actionBack, "close"),
		}
	case m.visualActive():
		return []footerHint{
			hint(actionDelete, "delete"),
			hint(actionToggleFlag, "flag"),
			hint(actionToggleRead, "read"),
			hint(actionQuit, "quit"),
			hint(actionBack, "cancel"),
			hintPair(actionUp, actionDown, "extend"),
		}
	case m.focus == focusReader:
		hints := []footerHint{
			hint(actionDelete, "delete"),
			hint(actionToggleFlag, "flag"),
			hint(actionToggleRead, "read"),
			hint(actionQuit, "quit"),
			hint(actionNextThreadMessage, "next"),
			hint(actionPrevThreadMessage, "prev"),
			hint(actionBack, "back"),
		}
		if overlay, ok := m.foldersOverlayHint(); ok {
			hints = append(hints, overlay)
		}

		return hints
	}

	hints := []footerHint{
		hint(actionDelete, "delete"),
		hint(actionToggleFlag, "flag"),
		hint(actionToggleRead, "read"),
		hint(actionOpen, "open"),
		hint(actionQuit, "quit"),
	}

	// A filtered list leads with the query, because a narrowed list that does not
	// say why looks like missing mail. The clear hint sits next to it: the two only
	// make sense together, and both outrank the keys that save a keystroke.
	if m.focus == focusMessages && m.filter != "" {
		hints = append([]footerHint{literalHint("filter", m.filter), hint(actionBack, "clear")}, hints...)
	}

	if overlay, ok := m.foldersOverlayHint(); ok {
		hints = append(hints, overlay)
	}

	return hints
}

// foldersOverlayHint reports whether the folders overlay key has to outrank the
// long tail. In two-column mode the folders panel is not on screen, so the
// overlay is the only way to reach it: the key that prevents being stuck beats
// the keys that save a keystroke, exactly like the priority set above it.
func (m Model) foldersOverlayHint() (footerHint, bool) {
	if m.layout().Mode != ModeTwo {
		return footerHint{}, false
	}

	return hint(actionFoldersOverlay, "folders"), true
}
