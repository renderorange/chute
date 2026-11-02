package config

import (
	"reflect"
	"strings"
	"testing"
)

// loadWithKeys appends a [keys] table to the shared valid config.
func loadWithKeys(t *testing.T, body string) (*Config, error) {
	t.Helper()

	return Load(writeConfig(t, validConfig+"\n[keys]\n"+body, 0o600))
}

// TestKeysOverrideReplacesTheDefault proves [keys] replaces an action's shipped keys
// rather than adding to them, and that the keys are trimmed on the way in.
func TestKeysOverrideReplacesTheDefault(t *testing.T) {
	cfg, err := loadWithKeys(t, "reload = [\"  ctrl+r  \", \"ctrl+shift+r\"]\n")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	want := []string{"ctrl+r", "ctrl+shift+r"}
	if got := cfg.KeyBindings()[KeyReload]; !reflect.DeepEqual(got, want) {
		t.Errorf("reload = %v, want %v", got, want)
	}
	if got := cfg.KeyBindings()[KeyQuit]; !reflect.DeepEqual(got, []string{"q"}) {
		t.Errorf("quit = %v, want its untouched default q", got)
	}
}

// TestKeysCanBindANewKey proves an override may use a key no default uses.
func TestKeysCanBindANewKey(t *testing.T) {
	cfg, err := loadWithKeys(t, "delete = [\"x\"]\n")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if got := cfg.KeyBindings()[KeyDelete]; !reflect.DeepEqual(got, []string{"x"}) {
		t.Errorf("delete = %v, want [x]", got)
	}
}

// TestKeyBindingsWithoutOverridesAreTheDefaults proves an absent [keys] table leaves the
// shipped map exactly as it was.
func TestKeyBindingsWithoutOverridesAreTheDefaults(t *testing.T) {
	cfg, err := Load(writeConfig(t, validConfig, 0o600))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	got, want := cfg.KeyBindings(), KeyDefaults()
	if !reflect.DeepEqual(got, want) {
		t.Errorf("bindings = %v, want the shipped defaults %v", got, want)
	}
}

// TestKeysRejectsAnUnknownAction is the issue's "unknown action name ... naming the
// offending entry": a typo must fail, not silently keep the default.
func TestKeysRejectsAnUnknownAction(t *testing.T) {
	_, err := loadWithKeys(t, "reloads = [\"ctrl+r\"]\n")
	if err == nil {
		t.Fatal("Load accepted an unknown action")
	}

	for _, want := range []string{"[keys]", `"reloads"`, "unknown"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error = %q, want it to contain %q", err, want)
		}
	}
}

// TestKeysRejectsAnEmptyAction is the issue's "removing every key from an action is
// rejected": an action with no keys could never be reached or advertised.
func TestKeysRejectsAnEmptyAction(t *testing.T) {
	for _, body := range []string{"quit = []\n", "quit = [\"  \"]\n"} {
		_, err := loadWithKeys(t, body)
		if err == nil {
			t.Fatalf("Load accepted %q", body)
		}
		if !strings.Contains(err.Error(), `"quit"`) {
			t.Errorf("error for %q = %q, want it to name quit", body, err)
		}
	}
}

// TestKeysRejectsAKeyTwiceInOneAction keeps the help screen honest: the same key listed
// twice would render twice and mean nothing extra.
func TestKeysRejectsAKeyTwiceInOneAction(t *testing.T) {
	_, err := loadWithKeys(t, "quit = [\"x\", \"x\"]\n")
	if err == nil {
		t.Fatal("Load accepted a duplicate key in one action")
	}

	for _, want := range []string{`"quit"`, `"x"`, "twice"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error = %q, want it to contain %q", err, want)
		}
	}
}

// TestKeysRejectsTheSameKeyForTwoOverrides is the issue's headline validation rule.
func TestKeysRejectsTheSameKeyForTwoOverrides(t *testing.T) {
	_, err := loadWithKeys(t, "quit = [\"x\"]\nreload = [\"x\"]\n")
	if err == nil {
		t.Fatal("Load accepted one key bound to two actions")
	}

	for _, want := range []string{`"x"`, `"quit"`, `"reload"`} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error = %q, want it to contain %q", err, want)
		}
	}
}

// TestKeysRejectsAKeyStolenFromADefault is the case a table-only check would miss:
// delete = ["q"] collides with quit's shipped key, and resolving that silently would be
// the first-wins the issue forbids.
func TestKeysRejectsAKeyStolenFromADefault(t *testing.T) {
	_, err := loadWithKeys(t, "delete = [\"q\"]\n")
	if err == nil {
		t.Fatal("Load accepted an override that steals quit's key")
	}

	for _, want := range []string{`"q"`, `"quit"`, `"delete"`} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error = %q, want it to contain %q", err, want)
		}
	}
}

// TestKeyActionNamesAreStable proves the published name list is sorted and holds the
// names the shipped map uses: a caller can rely on iteration order.
func TestKeyActionNamesAreStable(t *testing.T) {
	names := KeyActionNames()

	if len(names) != len(KeyDefaults()) {
		t.Errorf("names = %d, defaults = %d", len(names), len(KeyDefaults()))
	}

	for i := 1; i < len(names); i++ {
		if names[i-1] >= names[i] {
			t.Fatalf("names are not sorted: %q then %q", names[i-1], names[i])
		}
	}

	seen := make(map[string]bool, len(names))
	for _, name := range names {
		seen[name] = true
	}
	for _, want := range []string{KeyQuit, KeyReload, KeyDelete, KeyFilter, KeyHelp} {
		if !seen[want] {
			t.Errorf("KeyActionNames is missing %q", want)
		}
	}
}
