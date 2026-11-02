package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
)

// withStateDir points XDG_STATE_HOME at a temp directory and returns it.
func withStateDir(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	t.Setenv("XDG_STATE_HOME", dir)

	return dir
}

// TestStatePathUsesXDGStateHome proves the file lives where the issue says.
func TestStatePathUsesXDGStateHome(t *testing.T) {
	dir := withStateDir(t)

	path, err := StatePath()
	if err != nil {
		t.Fatalf("StatePath: %v", err)
	}
	if want := filepath.Join(dir, "chute", "state.toml"); path != want {
		t.Errorf("path = %q, want %q", path, want)
	}
}

// TestStatePathFallsBackToLocalState covers an unset XDG_STATE_HOME.
func TestStatePathFallsBackToLocalState(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_STATE_HOME", "")

	path, err := StatePath()
	if err != nil {
		t.Fatalf("StatePath: %v", err)
	}
	if want := filepath.Join(home, ".local", "state", "chute", "state.toml"); path != want {
		t.Errorf("path = %q, want %q", path, want)
	}
}

// TestStatePathIgnoresARelativeXDGStateHome follows the XDG rule: a relative value is
// not a state directory, so it is ignored rather than used relative to the cwd.
func TestStatePathIgnoresARelativeXDGStateHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_STATE_HOME", "relative/state")

	path, err := StatePath()
	if err != nil {
		t.Fatalf("StatePath: %v", err)
	}
	if want := filepath.Join(home, ".local", "state", "chute", "state.toml"); path != want {
		t.Errorf("path = %q, want the home fallback %q", path, want)
	}
}

// TestSaveStateRoundTrips proves what is written is what is read, and that the file is
// private: it holds the user's mail folders, which is nobody else's business.
func TestSaveStateRoundTrips(t *testing.T) {
	dir := withStateDir(t)

	want := State{
		Account: "work",
		Folders: map[string]string{"work": "Archive", "fastmail": "INBOX"},
	}
	if err := SaveState(want); err != nil {
		t.Fatalf("SaveState: %v", err)
	}

	path := filepath.Join(dir, "chute", "state.toml")

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Errorf("mode = %04o, want 0600", got)
	}

	if got := LoadState(); !reflect.DeepEqual(got, want) {
		t.Errorf("loaded = %+v, want %+v", got, want)
	}

	// A second write replaces the file without changing its mode.
	next := State{Account: "fastmail", Folders: map[string]string{"fastmail": "Sent"}}
	if err := SaveState(next); err != nil {
		t.Fatalf("second SaveState: %v", err)
	}
	if got := LoadState(); !reflect.DeepEqual(got, next) {
		t.Errorf("loaded after rewrite = %+v, want %+v", got, next)
	}
	if info, err = os.Stat(path); err != nil {
		t.Fatalf("stat after rewrite: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Errorf("mode after rewrite = %04o, want 0600", got)
	}
}

// TestStateFileHoldsOnlyNames is the issue's privacy criterion: account and folder
// names, and nothing that could carry a credential or message content.
func TestStateFileHoldsOnlyNames(t *testing.T) {
	dir := withStateDir(t)

	if err := SaveState(State{Account: "work", Folders: map[string]string{"work": "Archive"}}); err != nil {
		t.Fatalf("SaveState: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "chute", "state.toml"))
	if err != nil {
		t.Fatalf("reading state: %v", err)
	}

	var raw map[string]any
	if _, err := toml.Decode(string(data), &raw); err != nil {
		t.Fatalf("decoding state: %v", err)
	}

	if len(raw) != 2 {
		t.Fatalf("top-level keys = %v, want exactly account and folders", raw)
	}
	if raw["account"] != "work" {
		t.Errorf("account = %v, want work", raw["account"])
	}

	folders, ok := raw["folders"].(map[string]any)
	if !ok {
		t.Fatalf("folders = %T, want a table", raw["folders"])
	}
	if len(folders) != 1 || folders["work"] != "Archive" {
		t.Errorf("folders = %v, want work = Archive", folders)
	}

	for _, forbidden := range []string{"password", "username", "imap", "smtp"} {
		if strings.Contains(strings.ToLower(string(data)), forbidden) {
			t.Errorf("state file mentions %q:\n%s", forbidden, data)
		}
	}
}

// TestSaveStateCreatesItsDirectory proves a first run does not need the directory to
// exist, and that the directory itself is private.
func TestSaveStateCreatesItsDirectory(t *testing.T) {
	dir := withStateDir(t)

	if err := SaveState(State{Account: "fastmail"}); err != nil {
		t.Fatalf("SaveState: %v", err)
	}

	info, err := os.Stat(filepath.Join(dir, "chute"))
	if err != nil {
		t.Fatalf("stat dir: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o700 {
		t.Errorf("directory mode = %04o, want 0700", got)
	}
}

// TestLoadStateWithoutAFileIsEmpty is the first-run case: no file is not an error.
func TestLoadStateWithoutAFileIsEmpty(t *testing.T) {
	withStateDir(t)

	if got := LoadState(); !reflect.DeepEqual(got, State{}) {
		t.Errorf("loaded = %+v, want the empty state", got)
	}
}

// TestLoadStateWithACorruptFileIsEmpty is the issue's "corrupt or unparsable ... falls
// back to defaults without a crash".
func TestLoadStateWithACorruptFileIsEmpty(t *testing.T) {
	dir := withStateDir(t)

	path := filepath.Join(dir, "chute", "state.toml")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte("account = [unterminated\n\n[[["), 0o600); err != nil {
		t.Fatalf("writing corrupt state: %v", err)
	}

	if got := LoadState(); !reflect.DeepEqual(got, State{}) {
		t.Errorf("loaded = %+v, want the empty state", got)
	}
}
