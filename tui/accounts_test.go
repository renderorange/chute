package tui

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/renderorange/chute/config"
	"github.com/renderorange/chute/mail"
)

// fakeUnseen counts unseen messages from canned per-account values and records
// every folder it was asked about, so a test can prove the count is INBOX only.
func fakeUnseen(counts map[string]uint32, errs map[string]error) (unseenFunc, *[]string) {
	asked := &[]string{}

	fn := func(acct config.Account, folder string) (uint32, error) {
		*asked = append(*asked, acct.Name+"/"+folder)

		if err := errs[acct.Name]; err != nil {
			return 0, err
		}

		return counts[acct.Name], nil
	}

	return fn, asked
}

// openSelector drives a and returns the model with the selector open.
func openSelector(t *testing.T, m Model) Model {
	t.Helper()

	next, _ := m.Update(keyMsg("a"))
	open, ok := next.(Model)
	if !ok {
		t.Fatal("update did not return a Model")
	}
	if !open.accountsOpen {
		t.Fatal("a did not open the account selector")
	}

	return open
}

func TestAccountKeyOpensSelector(t *testing.T) {
	m := openSelector(t, sized(140, 40))

	if m.accountCursor != m.account {
		t.Errorf("cursor = %d, want the active account %d", m.accountCursor, m.account)
	}
	if m.status != "" {
		t.Errorf("status = %q, want empty", m.status)
	}
}

func TestAccountSelectorListsEveryAccount(t *testing.T) {
	view := openSelector(t, sized(140, 40)).View()

	for _, want := range []string{"fastmail", "work", "enter:switch", "esc:close"} {
		if !strings.Contains(view, want) {
			t.Errorf("selector view missing %q", want)
		}
	}
}

func TestAccountSelectorMovesAndCloses(t *testing.T) {
	m := openSelector(t, sized(140, 40))

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = next.(Model)
	if m.accountCursor != 1 {
		t.Errorf("cursor after down = %d, want 1", m.accountCursor)
	}

	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	if got := next.(Model).accountCursor; got != 1 {
		t.Errorf("cursor past the last account = %d, want 1", got)
	}

	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyUp})
	if got := next.(Model).accountCursor; got != 0 {
		t.Errorf("cursor after up = %d, want 0", got)
	}

	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if next.(Model).accountsOpen {
		t.Error("esc did not close the selector")
	}
}

func TestAccountSelectorTogglesClosed(t *testing.T) {
	m := openSelector(t, sized(140, 40))

	next, _ := m.Update(keyMsg("a"))
	if next.(Model).accountsOpen {
		t.Error("a did not close the selector it opened")
	}
}

func TestSelectingAccountSwitchesAndResets(t *testing.T) {
	m := sized(140, 40)
	m.folders = sampleFolders(3)
	m.folderCursor = 2
	m.openFolder = "INBOX"
	m.messages = sampleMessages(2)
	m.rows = BuildRows(m.messages)
	m.bodies = map[bodyKey]*mail.Body{{account: "fastmail", folder: "INBOX", uid: 1}: {}}
	m.markSeenOnLoad = map[bodyKey]bool{{account: "fastmail", folder: "INBOX", uid: 1}: true}
	m.started = true

	m = openSelector(t, m)
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	switched, cmd := next.(Model).Update(tea.KeyMsg{Type: tea.KeyEnter})

	got := switched.(Model)
	if got.account != 1 {
		t.Errorf("account = %d, want 1", got.account)
	}
	if got.accountsOpen {
		t.Error("selecting an account left the selector open")
	}
	if got.folders != nil {
		t.Error("folders from the previous account survived the switch")
	}
	if got.openFolder != "" {
		t.Errorf("openFolder = %q, want empty", got.openFolder)
	}
	if len(got.messages) != 0 || len(got.rows) != 0 {
		t.Error("messages from the previous account survived the switch")
	}
	if len(got.bodies) != 0 {
		t.Error("the body cache from the previous account survived the switch")
	}
	if len(got.markSeenOnLoad) != 0 {
		t.Error("the mark-seen set from the previous account survived the switch")
	}
	if got.started {
		t.Error("started must reset so the new account opens its start folder")
	}
	if got.state != stateConnecting {
		t.Errorf("state = %v, want connecting", got.state)
	}
	if cmd == nil {
		t.Fatal("switching returned no folder-load command")
	}
	if _, ok := cmd().(foldersMsg); !ok {
		t.Errorf("switch command produced %T, want foldersMsg", cmd())
	}
}

func TestSelectingTheActiveAccountClosesWithoutReload(t *testing.T) {
	m := openSelector(t, sized(140, 40))

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		t.Error("selecting the active account should not reload folders")
	}
	if next.(Model).accountsOpen {
		t.Error("selecting the active account left the selector open")
	}
}

func TestSwitchDropsLateFolderResult(t *testing.T) {
	m := sized(140, 40)
	m = openSelector(t, m)
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	switched, _ := next.(Model).Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = switched.(Model)

	after, _ := m.Update(foldersMsg{account: "fastmail", folders: sampleFolders(3)})

	got := after.(Model)
	if len(got.folders) != 0 {
		t.Errorf("folders = %v, want none: the previous account's load must be dropped", got.folders)
	}
	if got.state != stateConnecting {
		t.Errorf("state = %v, want connecting", got.state)
	}
}

func TestSwitchOpensStartFolderForNewAccount(t *testing.T) {
	cfg := testConfig()
	inbox := "INBOX"
	cfg.Accounts[1].StartFolder = &inbox

	m := New(cfg, "test")
	next, _ := m.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
	m = next.(Model)

	m = openSelector(t, m)
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	switched, _ := next.(Model).Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = switched.(Model)

	after, cmd := m.Update(foldersMsg{account: "work", folders: sampleFolders(3)})

	got := after.(Model)
	if got.openFolder != "INBOX" {
		t.Errorf("openFolder = %q, want INBOX: the new account's start folder must open", got.openFolder)
	}
	if !got.msgLoading {
		t.Error("the start folder open did not start a message load")
	}
	if cmd == nil {
		t.Fatal("opening the start folder returned no command")
	}
}

func TestAccountKeyFetchesUnseenForEveryAccount(t *testing.T) {
	m := sized(140, 40)
	fn, asked := fakeUnseen(map[string]uint32{"fastmail": 3, "work": 7}, nil)
	m.countUnseen = fn

	_, cmd := m.Update(keyMsg("a"))
	if cmd == nil {
		t.Fatal("a returned no command")
	}

	batch, ok := cmd().(tea.BatchMsg)
	if !ok {
		t.Fatalf("a command produced %T, want tea.BatchMsg", cmd())
	}
	if len(batch) != len(m.cfg.Accounts) {
		t.Fatalf("batch holds %d commands, want one per account (%d)", len(batch), len(m.cfg.Accounts))
	}

	seen := make(map[string]uint32, len(batch))
	for _, sub := range batch {
		msg, ok := sub().(unseenMsg)
		if !ok {
			t.Fatalf("sub-command produced %T, want unseenMsg", sub())
		}
		seen[msg.account] = msg.count
	}

	if seen["fastmail"] != 3 || seen["work"] != 7 {
		t.Errorf("counts = %v, want fastmail 3 and work 7", seen)
	}
	for _, want := range []string{"fastmail/INBOX", "work/INBOX"} {
		if !containsString(*asked, want) {
			t.Errorf("counts asked %v, missing %q", *asked, want)
		}
	}
}

func TestUnseenCountsRenderInSelector(t *testing.T) {
	m := openSelector(t, sized(140, 40))

	next, _ := m.Update(unseenMsg{account: "fastmail", count: 12})
	next, _ = next.(Model).Update(unseenMsg{account: "work", count: 0})

	view := next.(Model).View()
	if !strings.Contains(view, "12 unread") {
		t.Errorf("selector view missing the fastmail count:\n%s", view)
	}
	if !strings.Contains(view, "0 unread") {
		t.Errorf("selector view missing the work count:\n%s", view)
	}
}

func TestUnseenFailureStaysSelectable(t *testing.T) {
	m := openSelector(t, sized(140, 40))

	next, _ := m.Update(unseenMsg{account: "work", err: errors.New("dial tcp: connection refused")})
	got := next.(Model)

	if !got.accountsOpen {
		t.Fatal("an unread failure closed the selector")
	}
	if view := got.View(); !strings.Contains(view, "connection refused") {
		t.Errorf("selector view missing the failure:\n%s", view)
	}

	// The account must still be selectable: a dead server is not a dead row.
	next, _ = got.Update(tea.KeyMsg{Type: tea.KeyDown})
	switched, _ := next.(Model).Update(tea.KeyMsg{Type: tea.KeyEnter})
	if switched.(Model).account != 1 {
		t.Error("an account whose count failed could not be selected")
	}
}

// TestFooterDoesNotAdvertiseTheAccountKey pins the #26 decision that the footer
// carries only the keys that prevent a mistake. The account switcher still works
// on `a`; it is documented in the help screen (#21) rather than the footer, which
// the owner reported as overwhelming. If the footer ever grows a hint again, this
// test should be rewritten with the issue that reintroduces it.
func TestFooterDoesNotAdvertiseTheAccountKey(t *testing.T) {
	view := sized(140, 40).View()

	if strings.Contains(view, "a:account") {
		t.Errorf("footer advertises the account key again:\n%s", view)
	}
}

// containsString reports whether want is in list.
func containsString(list []string, want string) bool {
	for _, item := range list {
		if item == want {
			return true
		}
	}

	return false
}

// TestAccountSelectorKeepsTheLineWidthInvariant proves the dialog does not break the
// frame geometry the rest of the view is pinned to. Every rendered line must still
// be exactly the terminal width, including at the 60x20 floor.
func TestAccountSelectorKeepsTheLineWidthInvariant(t *testing.T) {
	for _, size := range []struct{ width, height int }{
		{60, 20},
		{105, 40},
		{130, 40},
	} {
		m := openSelector(t, sized(size.width, size.height))
		lines := strings.Split(m.View(), "\n")

		if len(lines) != size.height {
			t.Errorf("%dx%d: view has %d lines, want %d", size.width, size.height, len(lines), size.height)
		}
		for i, line := range lines {
			if got := lipgloss.Width(line); got != size.width {
				t.Errorf("%dx%d: line %d is %d cells, want %d", size.width, size.height, i, got, size.width)
				break
			}
		}
	}
}

// threeAccounts extends the shipped two-account config, so a cycle has somewhere to wrap.
func threeAccounts() *config.Config {
	cfg := testConfig()
	cfg.Accounts = append(cfg.Accounts, config.Account{Name: "third", StartFolder: startFolder("")})

	return cfg
}

// TestCycleAccountsWrapsBothWays is #56: ] walks forward, [ walks back, and the ring wraps
// with two or more accounts, which is the whole point of the key.
func TestCycleAccountsWrapsBothWays(t *testing.T) {
	m := sizedWith(threeAccounts(), 140, 40)

	for _, want := range []int{1, 2, 0} {
		next, _ := m.Update(keyMsg("]"))
		m = next.(Model)

		if m.account != want {
			t.Fatalf("after ], account = %d, want %d", m.account, want)
		}
	}

	for _, want := range []int{2, 1, 0} {
		next, _ := m.Update(keyMsg("["))
		m = next.(Model)

		if m.account != want {
			t.Fatalf("after [, account = %d, want %d", m.account, want)
		}
	}
}

// TestCycleAccountsResetsTheSwitch proves the cycle uses the modal's switch path: the
// previous account's folders, messages and body cache do not leak into the next one, and the
// start folder is re-armed.
func TestCycleAccountsResetsTheSwitch(t *testing.T) {
	m := sizedWith(threeAccounts(), 140, 40)
	m.folders = sampleFolders(3)
	m.openFolder = "INBOX"
	m.messages = sampleMessages(2)
	m.rows = BuildRows(m.messages)
	m.bodies = map[bodyKey]*mail.Body{{account: "fastmail", folder: "INBOX", uid: 1}: {}}
	m.markSeenOnLoad = map[bodyKey]bool{{account: "fastmail", folder: "INBOX", uid: 1}: true}
	m.started = true

	next, cmd := m.Update(keyMsg("]"))
	m = next.(Model)

	if m.account != 1 {
		t.Fatalf("account = %d, want 1", m.account)
	}
	if cmd == nil {
		t.Fatal("the cycle returned no folder load")
	}
	if m.folders != nil || len(m.messages) != 0 || len(m.bodies) != 0 || len(m.markSeenOnLoad) != 0 {
		t.Error("the previous account's state survived the cycle")
	}
	if m.openFolder != "" || m.started {
		t.Errorf("openFolder = %q, started = %v, want the new account's start folder", m.openFolder, m.started)
	}

	// Back to the first account, then forward again: the second visit must be as clean as
	// the first, which catches state that was only cleared on the way out.
	next, _ = m.Update(keyMsg("["))
	m = next.(Model)
	if m.account != 0 {
		t.Fatalf("account = %d, want 0", m.account)
	}
	if m.folders != nil || len(m.messages) != 0 || m.started {
		t.Error("the second switch reused the other account's state")
	}
}

// TestCycleAccountsWithOneAccountIsInert: no movement and no status noise.
func TestCycleAccountsWithOneAccountIsInert(t *testing.T) {
	cfg := testConfig()
	cfg.Accounts = cfg.Accounts[:1]
	m := sizedWith(cfg, 140, 40)

	for _, key := range []string{"]", "["} {
		next, cmd := m.Update(keyMsg(key))
		m = next.(Model)

		if cmd != nil {
			t.Errorf("%s started a load with one account", key)
		}
		if m.account != 0 || m.status != "" {
			t.Errorf("%s: account = %d, status = %q, want 0 and empty", key, m.account, m.status)
		}
	}
}

// TestCycleKeepsTheSelectorCursorInStep proves the modal and the key agree: after a cycle,
// opening the selector starts on the account the user is looking at.
func TestCycleKeepsTheSelectorCursorInStep(t *testing.T) {
	m := sizedWith(threeAccounts(), 140, 40)

	next, _ := m.Update(keyMsg("]"))
	m = next.(Model)

	if m.accountCursor != m.account {
		t.Errorf("selector cursor = %d, want the active account %d", m.accountCursor, m.account)
	}
}

// TestCycleKeyCanBeRebound is the M6 property applied to the new actions: the handler follows
// the binding, not the bracket.
func TestCycleKeyCanBeRebound(t *testing.T) {
	m := sizedWith(threeAccounts(), 140, 40)

	keys := defaultBindings()
	keys[actionAccountNext] = []string{"ctrl+n"}
	m.keys = keys

	next, _ := m.Update(keyMsg("ctrl+n"))
	m = next.(Model)

	if m.account != 1 {
		t.Errorf("account = %d, want 1 after the rebound key", m.account)
	}

	next, cmd := m.Update(keyMsg("]"))
	m = next.(Model)
	if cmd != nil || m.account != 1 {
		t.Error("the replaced default still cycled the account")
	}
}
