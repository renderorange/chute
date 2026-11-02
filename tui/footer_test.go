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

// footerDefaultHints is the default footer at every supported width, hint for
// hint. It is asserted whole rather than by substring so a hint that grows past
// the budget fails here instead of being silently dropped. The owner asked for
// this set to shrink to the mistake-preventing keys (#26): the footer is a
// reminder, not the keymap, and the rest waits for the help screen.
const footerDefaultHints = "d:delete f:flag m:read enter:open q:quit"

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
// than from a second copy of the keymap: rebinding delete moves the hint with it.
func TestFooterFollowsARebind(t *testing.T) {
	m := sized(105, 40)
	m.keys[actionDelete] = []string{"x"}

	footer := m.renderFooter()

	if !strings.Contains(footer, "x:delete") {
		t.Errorf("footer does not follow the rebind: %q", footer)
	}
	if strings.Contains(footer, "d:delete") {
		t.Errorf("footer still advertises the old key: %q", footer)
	}
}

// TestFooterRemovedBindingIsDropped proves a hint disappears when its action has
// no keys, instead of rendering a bare ":label".
func TestFooterRemovedBindingIsDropped(t *testing.T) {
	m := sized(105, 40)
	m.keys[actionToggleFlag] = nil

	footer := m.renderFooter()

	if strings.Contains(footer, "flag") {
		t.Errorf("footer names an action that has no binding: %q", footer)
	}
}

// TestFooterAt105IsTheFullSet pins the tablet footer exactly: the priority keys,
// nothing else. #26 cut this from ten hints to five on the owner's report that
// the footer was overwhelming.
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

// TestFooterAt60KeepsWholeHints proves the narrow footer drops nothing at the
// supported floor: the five hints fit in 54 cells.
func TestFooterAt60KeepsWholeHints(t *testing.T) {
	m := sized(60, 40)
	footer := m.renderFooter()

	if footer != footerDefaultHints {
		t.Errorf("footer at 60 = %q, want %q", footer, footerDefaultHints)
	}
	if got, limit := lipgloss.Width(footer), ContentWidth(60); got > limit {
		t.Errorf("footer at 60 is %d cells, want at most %d", got, limit)
	}
}

// TestFooterDropsWholeHintsWhenItMust keeps the packing rule covered. The default
// set fits from 60 columns up, so the drop path is exercised just below it: at 40
// columns the quit hint no longer fits and is dropped whole, never cut.
func TestFooterDropsWholeHintsWhenItMust(t *testing.T) {
	m := sized(40, 20)
	footer := m.renderFooter()

	const want = "d:delete f:flag m:read enter:open"
	if footer != want {
		t.Errorf("footer at 40 = %q, want %q", footer, want)
	}
	if !strings.HasPrefix(footerDefaultHints, footer) {
		t.Errorf("footer at 40 is not a whole-hint prefix of the full set: %q", footer)
	}
	if strings.Contains(footer, "...") {
		t.Errorf("footer truncated a hint: %q", footer)
	}
	if strings.Contains(footer, "q:quit") {
		t.Errorf("footer kept a hint that cannot fit: %q", footer)
	}
}

// TestFooterWideStaysCalm is the other half of #26: a wider terminal must not
// invite more hints back in. The set is fixed; the width is only a safety net.
func TestFooterWideStaysCalm(t *testing.T) {
	for _, width := range []int{120, 130, 140, 200} {
		if got := sized(width, 40).renderFooter(); got != footerDefaultHints {
			t.Errorf("footer at %d = %q, want the five-hint set", width, got)
		}
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
		"reader":          {"n:next", "p:prev", "q:quit"},
		"reader narrow":   {"n:next", "p:prev"},
		"visual range":    {"up/down:extend", "esc:cancel", "d:delete"},
		"confirm delete":  {"enter:delete", "esc:cancel"},
		"confirm discard": {"enter:discard", "esc:keep editing"},
		"accounts":        {"up/down:choose", "enter:switch", "esc:close"},
		"attachments":     {"up/down:choose", "enter:save", "esc:close"},
		"folders overlay": {"up/down:choose", "enter:open", "esc:close"},
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

// TestNoHintNamesJOrK is the acceptance criterion the footer makes automatic:
// every hint is derived from the binding table, so removing the binding removes
// the hint. Stated as a test so a future literal hint cannot reintroduce j or k.
func TestNoHintNamesJOrK(t *testing.T) {
	for _, tc := range footerCases(t) {
		for _, hint := range strings.Split(tc.m.renderFooter(), " ") {
			key := hintKeyPart(hint)
			for _, part := range strings.Split(key, "/") {
				if part == "j" || part == "k" {
					t.Errorf("%s: footer names %q, which is no longer bound: %q", tc.name, part, hint)
				}
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
