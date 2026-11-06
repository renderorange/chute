package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/renderorange/chute/config"
	"github.com/renderorange/chute/mail"
)

// loadedWith returns a model whose first folder load has completed, built from cfg.
func loadedWith(t *testing.T, cfg *config.Config, folders []mail.Folder) Model {
	t.Helper()

	m := sizedWith(cfg, 140, 40)
	m.loadFolders = fakeFolders(folders, nil)

	cmd := m.Init()
	if cmd == nil {
		t.Fatal("Init returned no command")
	}

	next, _ := m.Update(cmd())

	return next.(Model)
}

// inboxWithKeys returns a model in an open INBOX whose config applies [keys].
func inboxWithKeys(t *testing.T, overrides map[string][]string) Model {
	t.Helper()

	cfg := testConfig()
	cfg.Keys = overrides

	m := loadedWith(t, cfg, []mail.Folder{{Name: "INBOX", Delimiter: '/'}})
	m.loadMessages = fakeMessages(sampleMessages(2), nil)

	return openInbox(t, m)
}

// openHelp presses ? and proves the overlay opened.
func openHelp(t *testing.T, m Model) Model {
	t.Helper()

	next, _ := m.Update(keyMsg("?"))
	m = next.(Model)

	if !m.helpOpen {
		t.Fatal("? did not open the help overlay")
	}

	return m
}

// closeHelp sends key and proves the overlay closed.
func closeHelp(t *testing.T, m Model, key string) Model {
	t.Helper()

	next, _ := m.Update(keyMsg(key))
	m = next.(Model)

	if m.helpOpen {
		t.Fatalf("%q did not close the help overlay", key)
	}

	return m
}

// TestHelpOpensAndListsTheContexts covers the issue's overlay and grouping. The
// terminal is tall enough that the whole map renders at once; the 60x20 scroll test
// covers the case where it does not fit.
func TestHelpOpensAndListsTheContexts(t *testing.T) {
	m := openHelp(t, sized(140, 80))
	view := m.View()

	for _, want := range []string{"keys", "global", "list", "selection", "reader", "composer", "modals"} {
		if !strings.Contains(view, want) {
			t.Errorf("help is missing %q:\n%s", want, view)
		}
	}
}

// TestHelpClosesWithQuestionMarkAndEsc is the issue's closing rule, and proves esc is
// consumed by the overlay rather than the main view's "back".
func TestHelpClosesWithQuestionMarkAndEsc(t *testing.T) {
	m := openHelp(t, sized(140, 50))
	m = closeHelp(t, m, "?")

	m = openHelp(t, m)
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = next.(Model)

	if m.helpOpen {
		t.Fatal("esc did not close the help overlay")
	}
}

// TestHelpDoesNotFallThroughToBack proves the overlay owns the key: closing help from
// the reader leaves focus in the reader instead of also walking back to the list.
func TestHelpDoesNotFallThroughToBack(t *testing.T) {
	m := openInbox(t, withInbox(t, sampleMessages(2), nil))
	m.focus = focusReader
	m = openHelp(t, m)

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = next.(Model)

	if m.focus != focusReader {
		t.Errorf("focus = %v, want the reader left alone by closing help", m.focus)
	}
}

// TestEveryActionIsNamedAndInHelp is the drift gate between the config vocabulary and
// the action enum: a new action has to be named and documented in both directions.
func TestEveryActionIsNamedAndInHelp(t *testing.T) {
	names := make(map[string]action, len(actionNames))
	for act, name := range actionNames {
		names[name] = act

		if _, ok := config.KeyDefaults()[name]; !ok {
			t.Errorf("action %v is named %q, which has no default entry", act, name)
		}
	}

	for name := range config.KeyDefaults() {
		if _, ok := names[name]; !ok {
			t.Errorf("config action %q has no tui action", name)
		}
	}

	covered := make(map[action]bool, len(actionNames))
	for _, group := range helpGroups() {
		for _, row := range group.rows {
			for _, act := range row.actions {
				covered[act] = true
			}
		}
	}

	for act, name := range actionNames {
		if !covered[act] {
			t.Errorf("action %q (%v) is missing from the help groups", name, act)
		}
	}
}

// TestHelpShowsAReboundKey proves help reads the resolved map: the new key appears and
// the replaced one does not.
func TestHelpShowsAReboundKey(t *testing.T) {
	m := inboxWithKeys(t, map[string][]string{
		config.KeyReload: {"ctrl+shift+r"},
	})
	m = openHelp(t, m)

	view := m.View()
	if !strings.Contains(view, "ctrl+shift+r") {
		t.Errorf("help does not show the rebound key:\n%s", view)
	}
	if strings.Contains(view, "ctrl+r") {
		t.Errorf("help still shows the replaced default:\n%s", view)
	}
}

// TestRebindAgreesInFooterHelpAndDispatch is the issue's acceptance criterion: the
// footer, the help screen and the handler all follow one [keys] entry.
func TestRebindAgreesInFooterHelpAndDispatch(t *testing.T) {
	m := inboxWithKeys(t, map[string][]string{config.KeyDelete: {"x"}})

	footer := m.renderFooter()
	if !strings.Contains(footer, "x:delete") {
		t.Errorf("footer = %q, want x:delete", footer)
	}
	if strings.Contains(footer, "d:delete") {
		t.Errorf("footer = %q, still advertising the replaced key", footer)
	}

	help := openHelp(t, m).View()
	if !strings.Contains(help, "x") || !strings.Contains(help, "delete") {
		t.Errorf("help does not show the rebound delete key:\n%s", help)
	}

	next, _ := m.Update(keyMsg("x"))
	if !next.(Model).confirmOpen {
		t.Error("the rebound key did not ask for confirmation")
	}

	next, _ = m.Update(keyMsg("d"))
	if next.(Model).confirmOpen {
		t.Error("the replaced key still started a delete")
	}
}

// TestHelpFitsANarrowTerminalWithAnAbsurdKey proves a [keys] entry cannot widen the
// frame: key strings are user input, and every line must stay exactly the terminal
// width at the 60x20 floor.
func TestHelpFitsANarrowTerminalWithAnAbsurdKey(t *testing.T) {
	cfg := testConfig()
	cfg.Keys = map[string][]string{config.KeyReload: {strings.Repeat("k", 500)}}

	m := sizedWith(cfg, 60, 20)
	m = openHelp(t, m)

	lines := strings.Split(m.View(), "\n")
	if len(lines) != 20 {
		t.Errorf("view has %d lines, want 20", len(lines))
	}
	for i, line := range lines {
		if got := lipgloss.Width(line); got != 60 {
			t.Fatalf("line %d is %d cells, want 60: %q", i, got, line)
		}
	}
}

// TestHelpScrollsAtSixtyByTwenty covers the issue's "fits at 60x20 by scrolling": the
// overlay is taller than the screen, keys move it, and it stops at both ends.
func TestHelpScrollsAtSixtyByTwenty(t *testing.T) {
	m := openHelp(t, sized(60, 20))
	top := m.View()

	if m.helpScroll != 0 {
		t.Errorf("helpScroll = %d, want 0 when it opens", m.helpScroll)
	}

	next, _ := m.Update(keyMsg("G"))
	m = next.(Model)

	if m.helpScroll == 0 {
		t.Fatal("end did not scroll the help")
	}
	if m.View() == top {
		t.Error("the view did not change after scrolling")
	}

	bottom := m.helpScroll
	next, _ = m.Update(keyMsg("G"))
	m = next.(Model)
	if m.helpScroll != bottom {
		t.Errorf("helpScroll = %d after scrolling past the end, want %d", m.helpScroll, bottom)
	}

	next, _ = m.Update(keyMsg("g"))
	m = next.(Model)
	if m.helpScroll != 0 {
		t.Errorf("helpScroll = %d after home, want 0", m.helpScroll)
	}
	if got := m.View(); got != top {
		t.Error("home did not restore the first screen of help")
	}
}

// TestQuestionMarkStaysLiteralInTheFilterPrompt proves help does not steal text input:
// a query may contain ?, and the overlay does not open mid-typing.
func TestQuestionMarkStaysLiteralInTheFilterPrompt(t *testing.T) {
	m := typeQuery(t, withFilter(t), "")

	next, _ := m.Update(keyMsg("?"))
	m = next.(Model)

	if m.helpOpen {
		t.Fatal("? opened help instead of entering the query")
	}
	if m.filter != "?" {
		t.Errorf("filter = %q, want the literal ?", m.filter)
	}
}
