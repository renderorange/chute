package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/renderorange/chute/config"
)

// readmePath is the shipped user documentation.
const readmePath = "README.md"

// readmeText reads the README, so every documentation test fails loudly when it is
// missing instead of skipping.
func readmeText(t *testing.T) string {
	t.Helper()

	raw, err := os.ReadFile(readmePath)
	if err != nil {
		t.Fatalf("reading %s: %v", readmePath, err)
	}

	return string(raw)
}

// readmeKeysSection returns the text between "## Keys" and the next level-two heading.
func readmeKeysSection(t *testing.T, readme string) string {
	t.Helper()

	start := strings.Index(readme, "\n## Keys\n")
	if start < 0 {
		t.Fatalf("%s has no \"## Keys\" section", readmePath)
	}
	rest := readme[start+1+len("## Keys\n"):]

	if end := strings.Index(rest, "\n## "); end >= 0 {
		rest = rest[:end]
	}

	return rest
}

// readmeActionRows parses the action tables in the Keys section into action -> keys. A
// row is an action row when its first cell is a backticked name, which is why the
// composer's literal keys are bullets rather than table rows.
func readmeActionRows(t *testing.T, readme string) map[string][]string {
	t.Helper()

	rows := make(map[string][]string)

	for _, line := range strings.Split(readmeKeysSection(t, readme), "\n") {
		if !strings.HasPrefix(line, "| `") {
			continue
		}

		cells := strings.Split(line, "|")
		if len(cells) < 3 {
			t.Errorf("unparseable key row: %q", line)

			continue
		}

		name := strings.Trim(strings.TrimSpace(cells[1]), "`")
		keys := make([]string, 0, 2)

		for _, key := range strings.Split(cells[2], ",") {
			if key = strings.Trim(strings.TrimSpace(key), "`"); key != "" {
				keys = append(keys, key)
			}
		}

		if len(keys) == 0 {
			t.Errorf("%s: action %q lists no default keys", readmePath, name)
		}
		if previous, seen := rows[name]; seen && strings.Join(previous, " ") != strings.Join(keys, " ") {
			t.Errorf("%s: action %q is documented twice with different keys: %v and %v",
				readmePath, name, previous, keys)
		}

		rows[name] = keys
	}

	return rows
}

// TestReadmeKeyTableMatchesTheKeymap is the M6 gate made mechanical: the documented keys
// are the shipped ones, every action is documented, and the README invents nothing the
// config rejects. It fails when a default changes, an action is added or one is removed.
func TestReadmeKeyTableMatchesTheKeymap(t *testing.T) {
	rows := readmeActionRows(t, readmeText(t))
	defaults := config.KeyDefaults()

	for name, want := range defaults {
		got, ok := rows[name]
		if !ok {
			t.Errorf("%s key table is missing action %q", readmePath, name)

			continue
		}
		if strings.Join(got, " ") != strings.Join(want, " ") {
			t.Errorf("%s documents %q as %v, want %v", readmePath, name, got, want)
		}
	}

	for name := range rows {
		if _, ok := defaults[name]; !ok {
			t.Errorf("%s documents action %q, which [keys] rejects", readmePath, name)
		}
	}
}

// TestReadmeNamesEveryFlag keeps the flag list honest as the command grows.
func TestReadmeNamesEveryFlag(t *testing.T) {
	readme := readmeText(t)

	for _, flag := range []string{"-config", "-check", "-debug", "-version"} {
		if !strings.Contains(readme, flag) {
			t.Errorf("%s does not document %s", readmePath, flag)
		}
	}
}

// TestReadmeNamesEveryPath proves the two files chute reads and writes are named, along
// with the permission rule that makes a wrong one an error.
func TestReadmeNamesEveryPath(t *testing.T) {
	readme := readmeText(t)

	for _, want := range []string{
		"~/.config/chute/config.toml",
		"$XDG_STATE_HOME/chute/state.toml",
		"~/.local/state/chute/state.toml",
		"0600",
		"config.example.toml",
	} {
		if !strings.Contains(readme, want) {
			t.Errorf("%s does not mention %q", readmePath, want)
		}
	}
}

// TestExampleConfigLoads proves the sample shipped as documentation is a legal config:
// only the hostnames and credentials are placeholders. A copy at the private mode the
// README tells the user to set must load without error.
func TestExampleConfigLoads(t *testing.T) {
	raw, err := os.ReadFile("config.example.toml")
	if err != nil {
		t.Fatalf("reading config.example.toml: %v", err)
	}

	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatalf("writing the example: %v", err)
	}

	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("the example config does not load: %v", err)
	}

	if len(cfg.Accounts) != 1 {
		t.Fatalf("%d accounts in the example, want 1", len(cfg.Accounts))
	}
	if cfg.Accounts[0].Name == "" || cfg.Accounts[0].IMAP.Host == "" {
		t.Error("the example account is missing a name or an IMAP host")
	}
	if len(cfg.List.Columns) == 0 {
		t.Error("the example does not show [list] columns")
	}
	if !strings.Contains(string(raw), "[keys]") {
		t.Error("the example does not show [keys]")
	}
}

// TestExampleConfigRefusesTheWorldReadableMode is the other half of the README's chmod
// instruction: the same file at a looser mode must be refused, so the documentation and
// the loader agree about why the chmod matters.
func TestExampleConfigRefusesTheWorldReadableMode(t *testing.T) {
	raw, err := os.ReadFile("config.example.toml")
	if err != nil {
		t.Fatalf("reading config.example.toml: %v", err)
	}

	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatalf("writing the example: %v", err)
	}

	if _, err := config.Load(path); err == nil {
		t.Fatal("the example loaded at 0644, so the documented chmod is not enforced")
	}
}
