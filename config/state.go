package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

// stateFilename is the last-selection file's name inside its directory.
const stateFilename = "state.toml"

// State is the last selection, remembered so a restart lands where the user left.
//
// It holds names only: which account was active, which folder each account had
// open, and which folders each account had expanded in the folder tree. No
// credentials, no addresses, no message data. The config remains the authority — a
// name that no longer exists there is simply ignored.
type State struct {
	// Account is the account that was active, by name. Empty means none was recorded.
	Account string `toml:"account"`

	// Folders maps an account name to the folder it had open. A missing entry means
	// the account has no recorded folder.
	Folders map[string]string `toml:"folders"`

	// Expanded maps an account name to the folders whose subfolders were shown. A
	// folder absent from the list is collapsed, which is also the default for a new
	// state file: the tree opens showing only its top level.
	Expanded map[string][]string `toml:"expanded"`
}

// StatePath is the last-selection file: $XDG_STATE_HOME/chute/state.toml, or
// ~/.local/state/chute/state.toml when XDG_STATE_HOME is unset or relative (the XDG
// specification says a relative value must be ignored).
func StatePath() (string, error) {
	dir := strings.TrimSpace(os.Getenv("XDG_STATE_HOME"))
	if !filepath.IsAbs(dir) {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("state: %w", err)
		}

		dir = filepath.Join(home, ".local", "state")
	}

	return filepath.Join(dir, "chute", stateFilename), nil
}

// LoadState reads the last selection. Every failure — no file yet, an unreadable
// file, a corrupt one — returns the empty State rather than an error: the selection
// is a convenience, and the config is the authority. None of those cases is something
// the user should be told about on every start.
func LoadState() State {
	path, err := StatePath()
	if err != nil {
		return State{}
	}

	var state State
	if _, err := toml.DecodeFile(path, &state); err != nil {
		return State{}
	}

	return state
}

// SaveState writes the selection, creating its directory. The file is 0600 and the
// directory 0700: the folder names of a mail account are private.
//
// The write goes through a temporary file in the same directory and a rename, so a
// process killed mid-write leaves the previous selection intact instead of half a
// file that the next start would silently discard.
func SaveState(state State) error {
	path, err := StatePath()
	if err != nil {
		return err
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("state: %w", err)
	}

	file, err := os.CreateTemp(dir, stateFilename+".*")
	if err != nil {
		return fmt.Errorf("state: %w", err)
	}

	tmp := file.Name()

	if err := toml.NewEncoder(file).Encode(state); err != nil {
		file.Close()
		os.Remove(tmp)

		return fmt.Errorf("state: %w", err)
	}
	if err := file.Close(); err != nil {
		os.Remove(tmp)

		return fmt.Errorf("state: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)

		return fmt.Errorf("state: %w", err)
	}

	return nil
}
