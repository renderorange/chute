package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

// footerCase is one footer context: a model in the state that selects it.
type footerCase struct {
	name string
	m    Model
}

// footerDefaultHints is the default footer at 105 columns, hint for hint. It is
// asserted whole rather than by substring so a hint that grows past the budget
// fails here instead of being silently dropped.
const footerDefaultHints = "d:delete f:flag m:read enter:open q:quit esc:back c:compose r:reply R:reply-all F:forward a:account"

// footerCases builds one model per footer context. Every model is 105 columns
// unless the case is about the composer, whose hints are wider.
func footerCases(t *testing.T) []footerCase {
	t.Helper()

	base := func(width int) Model {
		return sized(width, 40)
	}

	reader := base(105)
	reader.focus = focusReader

	readerNarrow := base(60)
	readerNarrow.focus = focusReader

	visual := base(105)
	visual.visualAnchor = 0

	confirmDeleteModel := base(105)
	confirmDeleteModel.confirmOpen = true
	confirmDeleteModel.confirmKind = confirmDelete

	confirmDiscardModel := base(105)
	confirmDiscardModel.confirmOpen = true
	confirmDiscardModel.confirmKind = confirmDiscardDraft

	accounts := base(105)
	accounts.accountsOpen = true

	attach := base(105)
	attach.attachOpen = true

	foldersOverlay := base(105)
	foldersOverlay.foldersOpen = true

	compose := base(120)
	compose.compose = &composer{}

	return []footerCase{
		{"default", base(105)},
		{"default narrow", base(60)},
		{"reader", reader},
		{"reader narrow", readerNarrow},
		{"visual range", visual},
		{"confirm delete", confirmDeleteModel},
		{"confirm discard", confirmDiscardModel},
		{"accounts", accounts},
		{"attachments", attach},
		{"folders overlay", foldersOverlay},
		{"composer", compose},
	}
}

// TestFooterKeysResolveToTheirActions is the lie test. Every hint in every
// context is split back into its keys and each key is resolved through the same
// binding table the update loop uses, so a hint can only appear if pressing the
// key it names reaches the action it advertises.
func TestFooterKeysResolveToTheirActions(t *testing.T) {
	for _, tc := range footerCases(t) {
		for _, hint := range tc.m.footerHints() {
			text := hintText(tc.m.keys, hint)

			if len(hint.actions) == 0 {
				if hint.literal == "" {
					t.Errorf("%s: hint %q has neither an action nor a literal key", tc.name, text)
				}

				continue
			}

			keys := strings.Split(hintKeyPart(text), "/")
			if len(keys) != len(hint.actions) {
				t.Errorf("%s: hint %q names %d keys for %d actions", tc.name, text, len(keys), len(hint.actions))

				continue
			}

			for i, key := range keys {
				if got := tc.m.keys.resolve(key); got != hint.actions[i] {
					t.Errorf("%s: hint %q names %q, which resolves to action %d, want action %d",
						tc.name, text, key, got, hint.actions[i])
				}
			}
		}
	}
}

// TestFooterFollowsARebind proves the hints come from the binding table rather
// than from a second copy of the keymap: rebinding reply moves the hint with it.
func TestFooterFollowsARebind(t *testing.T) {
	m := sized(105, 40)
	m.keys[actionReply] = []string{"z"}

	footer := m.renderFooter()

	if !strings.Contains(footer, "z:reply") {
		t.Errorf("footer does not follow the rebind: %q", footer)
	}
	if strings.Contains(footer, "r:reply") {
		t.Errorf("footer still advertises the old key: %q", footer)
	}
}

// TestFooterRemovedBindingIsDropped proves a hint disappears when its action has
// no keys, instead of rendering a bare ":label".
func TestFooterRemovedBindingIsDropped(t *testing.T) {
	m := sized(130, 40)
	m.keys[actionReplyAll] = nil

	footer := m.renderFooter()

	if strings.Contains(footer, "reply-all") {
		t.Errorf("footer names an action that has no binding: %q", footer)
	}
}

// TestFooterAt105IsTheFullSet pins the tablet footer exactly: the priority keys,
// back, and the four M5/M6 keys that had no hint at all.
func TestFooterAt105IsTheFullSet(t *testing.T) {
	m := sized(105, 40)

	if got := m.renderFooter(); got != footerDefaultHints {
		t.Errorf("footer at 105 =\n  %q\nwant\n  %q", got, footerDefaultHints)
	}
}

// TestFooterAt105FillsTheBudget proves the exact-fit string is legal: within the
// content width and never cut.
func TestFooterAt105FillsTheBudget(t *testing.T) {
	m := sized(105, 40)
	footer := m.renderFooter()

	if got, limit := lipgloss.Width(footer), ContentWidth(105); got > limit {
		t.Errorf("footer is %d cells, want at most %d: %q", got, limit, footer)
	}
	if strings.Contains(footer, "...") {
		t.Errorf("footer truncated a hint: %q", footer)
	}
}

// TestFooterAt60DropsWholeHints proves the narrow footer ends at a hint boundary
// rather than cutting one mid-token, which is what the old string did.
func TestFooterAt60DropsWholeHints(t *testing.T) {
	m := sized(60, 40)
	footer := m.renderFooter()

	const want = "d:delete f:flag m:read enter:open q:quit esc:back"
	if footer != want {
		t.Errorf("footer at 60 = %q, want %q", footer, want)
	}
	if !strings.HasPrefix(footerDefaultHints, footer) {
		t.Errorf("footer at 60 is not a whole-hint prefix of the 105 footer: %q", footer)
	}
	if strings.Contains(footer, "...") {
		t.Errorf("footer at 60 truncated a hint: %q", footer)
	}
	if got, limit := lipgloss.Width(footer), ContentWidth(60); got > limit {
		t.Errorf("footer at 60 is %d cells, want at most %d", got, limit)
	}
}

// TestFooterAt130ShowsControlRReload covers the other half of the defect: reload
// is ctrl+r, and the hint appears once the width allows it.
func TestFooterAt130ShowsControlRReload(t *testing.T) {
	m := sized(130, 40)
	footer := m.renderFooter()

	if !strings.Contains(footer, "ctrl+r:reload") {
		t.Errorf("footer at 130 does not show ctrl+r:reload: %q", footer)
	}
	if got, limit := lipgloss.Width(footer), ContentWidth(130); got > limit {
		t.Errorf("footer at 130 is %d cells, want at most %d: %q", got, limit, footer)
	}
}

// TestFooterNeverSaysRReload is the regression the issue was filed for. It scans
// whole hints, not substrings, because "ctrl+r:reload" contains "r:reload".
func TestFooterNeverSaysRReload(t *testing.T) {
	for _, width := range []int{60, 80, 95, 105, 115, 130, 140, 200} {
		for _, hint := range strings.Split(sized(width, 40).renderFooter(), " ") {
			if hint == "r:reload" {
				t.Errorf("width %d: the footer claims r:reload", width)
			}
		}
	}
}

// TestFooterModalContexts checks each context names the keys that work in it, and
// that none of them is truncated at the tablet width.
func TestFooterModalContexts(t *testing.T) {
	want := map[string][]string{
		"reader":          {"n:next", "p:prev", "s:attach", "q:quit"},
		"reader narrow":   {"n:next", "p:prev"},
		"visual range":    {"j/k:extend", "esc:cancel", "d:delete"},
		"confirm delete":  {"enter:delete", "esc:cancel"},
		"confirm discard": {"enter:discard", "esc:keep editing"},
		"accounts":        {"j/k:choose", "enter:switch", "esc:close"},
		"attachments":     {"j/k:choose", "enter:save", "esc:close"},
		"folders overlay": {"j/k:choose", "enter:open", "esc:close"},
	}

	for _, tc := range footerCases(t) {
		wants, ok := want[tc.name]
		if !ok {
			continue
		}

		footer := tc.m.renderFooter()
		for _, w := range wants {
			if !strings.Contains(footer, w) {
				t.Errorf("%s: footer is missing %q: %q", tc.name, w, footer)
			}
		}
		if tc.name != "reader narrow" {
			if got, limit := lipgloss.Width(footer), ContentWidth(tc.m.width); got > limit {
				t.Errorf("%s: footer is %d cells, want at most %d: %q", tc.name, got, limit, footer)
			}
		}
	}
}

// TestFooterComposerKeepsItsLiteralKeys covers the one context that cannot be
// derived: the composer reads its keys directly, outside the binding table.
func TestFooterComposerKeepsItsLiteralKeys(t *testing.T) {
	m := sized(120, 40)
	m.compose = &composer{}

	footer := m.renderFooter()

	for _, want := range []string{"ctrl+s:save", "ctrl+d:send", "tab:field", "esc:cancel"} {
		if !strings.Contains(footer, want) {
			t.Errorf("composer footer is missing %q: %q", want, footer)
		}
	}
}

// TestFooterReachesHiddenFolders pins the mode-aware rule: at 110 columns the
// folders panel is not on screen, so the overlay key outranks the long tail and
// stays in the footer. At 105 the panel is reachable, so the key is optional and
// does not spend the budget.
func TestFooterReachesHiddenFolders(t *testing.T) {
	two := sized(110, 40)
	if mode := two.layout().Mode; mode != ModeTwo {
		t.Fatalf("110 columns is mode %d, want ModeTwo", mode)
	}
	if footer := two.renderFooter(); !strings.Contains(footer, `\:folders`) {
		t.Errorf("two-column footer does not reach the hidden folders panel: %q", footer)
	}

	single := sized(105, 40)
	if mode := single.layout().Mode; mode != ModeSingle {
		t.Fatalf("105 columns is mode %d, want ModeSingle", mode)
	}
	if footer := single.renderFooter(); strings.Contains(footer, `\:folders`) {
		t.Errorf("single-column footer spends the budget on a panel that is on screen: %q", footer)
	}
}
