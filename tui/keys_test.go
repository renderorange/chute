package tui

import (
	"testing"
)

// navState is every cursor a navigation key can move. Comparing the whole tuple
// is what makes "does nothing" a real assertion rather than "the screen looked
// the same".
type navState struct {
	focus    panelFocus
	folders  int
	messages int
	reader   int
	overlay  int
	attach   int
	account  int
	visual   int
}

// navState reads the cursors that navigation may change.
func (m Model) navState() navState {
	return navState{
		focus:    m.focus,
		folders:  m.folderCursor,
		messages: m.messageCursor,
		reader:   m.readerScroll,
		overlay:  m.folderOverlayCursor,
		attach:   m.attachCursor,
		account:  m.accountCursor,
		visual:   m.visualAnchor,
	}
}

// TestNavigationKeysAreArrowsOnly is the claim #27 makes: j and k are not bound
// at all, and the arrows still are. No hint can name j or k once the binding is
// gone, because every hint is derived from this table.
func TestNavigationKeysAreArrowsOnly(t *testing.T) {
	b := defaultBindings()

	for _, key := range []string{"j", "k"} {
		if got := b.resolve(key); got != actionNone {
			t.Errorf("%q is still bound to action %d", key, got)
		}
	}

	for key, want := range map[string]action{
		"up":   actionUp,
		"down": actionDown,
		"g":    actionTop,
		"G":    actionBottom,
		"home": actionTop,
		"end":  actionBottom,
	} {
		if got := b.resolve(key); got != want {
			t.Errorf("%q resolves to action %d, want %d", key, got, want)
		}
	}
}

// TestMoveAndReadBindingsMatchTheIssue records #88's rebind: m moves a message, u
// toggles read/unread, and the old uppercase M is gone rather than left behind as a
// silent alias.
func TestMoveAndReadBindingsMatchTheIssue(t *testing.T) {
	b := defaultBindings()

	for key, want := range map[string]action{
		"m": actionMove,
		"u": actionToggleRead,
		"M": actionNone,
	} {
		if got := b.resolve(key); got != want {
			t.Errorf("%q resolves to action %d, want %d", key, got, want)
		}
	}
}

// jkContexts builds one model per surface #27 names. Each already has a movable
// cursor, so a bound j would visibly move it.
func jkContexts(t *testing.T) []footerCase {
	t.Helper()

	folders := loaded(t, 105, 59, sampleFolders(3), nil)

	messages := openInbox(t, withInbox(t, sampleMessages(3), nil))

	reader := readerThreadModel(t)

	overlay := loaded(t, 105, 59, sampleFolders(3), nil)
	overlay.foldersOpen = true

	attach := openSelectedBody(t, loadedBody(t, sampleMessages(1), sampleBody()))
	attach = press(t, attach, keyMsg("s"))

	visual := openInbox(t, withInbox(t, sampleMessages(3), nil))
	visual.visualAnchor = 0

	accounts := sized(105, 40)
	accounts.accountsOpen = true

	return []footerCase{
		{"folders", folders},
		{"messages", messages},
		{"reader", reader},
		{"folders overlay", overlay},
		{"attachments", attach},
		{"visual range", visual},
		{"accounts", accounts},
	}
}

// TestJAndKDoNothingInTheLists proves the bindings are not merely renamed: after
// j, and after k, no cursor in any list has moved. Each key is pressed on its own,
// because pressing j then k would cancel out on any bound pair and prove nothing.
func TestJAndKDoNothingInTheLists(t *testing.T) {
	for _, tc := range jkContexts(t) {
		before := tc.m.navState()

		for _, key := range []string{"j", "k"} {
			after := press(t, tc.m, keyMsg(key)).navState()
			if after != before {
				t.Errorf("%s: %q moved the cursor\n  before %+v\n  after  %+v", tc.name, key, before, after)
			}
		}
	}
}

// TestArrowsStillNavigate proves the removal did not take the arrows with it.
func TestArrowsStillNavigate(t *testing.T) {
	m := openInbox(t, withInbox(t, sampleMessages(3), nil))
	m.focus = focusMessages

	moved := press(t, m, namedKey("down"))
	if moved.messageCursor != 1 {
		t.Errorf("down did not move the message cursor: %d", moved.messageCursor)
	}

	back := press(t, moved, namedKey("up"))
	if back.messageCursor != 0 {
		t.Errorf("up did not move the message cursor back: %d", back.messageCursor)
	}
}

// TestJAndKStillTypeInTheComposer is the other half of the issue: this is about
// navigation bindings, so the letters must keep working as text where the app
// accepts text.
func TestJAndKStillTypeInTheComposer(t *testing.T) {
	m, _ := withComposeModel(t)
	m = press(t, m, keyMsg("c"))

	for i := 0; i < composerBody; i++ {
		m = press(t, m, namedKey("tab"))
	}

	m = press(t, m, keyMsg("j"))
	m = press(t, m, keyMsg("k"))

	if got := m.compose.body.text(); got != "jk" {
		t.Errorf("composer body = %q, want %q", got, "jk")
	}
}
