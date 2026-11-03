package config

// Keymap names and defaults for the [keys] config table.
//
// The names are the identifiers the TUI's handlers dispatch on, and the defaults are
// the shipped layout. Both live here, rather than in package tui, because [keys] has to
// be validated during config.Load and tui imports config, not the other way round. A
// tui test proves the two vocabularies stay in step.
import (
	"fmt"
	"sort"
	"strings"
)

// Action names accepted as keys in [keys]. Each is the stable identifier for one user
// intent; the TUI's action values map onto these strings.
const (
	KeyQuit              = "quit"
	KeyForceQuit         = "force_quit"
	KeyNextPanel         = "next_panel"
	KeyPrevPanel         = "prev_panel"
	KeyBack              = "back"
	KeyUp                = "up"
	KeyDown              = "down"
	KeyTop               = "top"
	KeyBottom            = "bottom"
	KeyOpen              = "open"
	KeyAttachments       = "attachments"
	KeyFoldersOverlay    = "folders_overlay"
	KeyReload            = "reload"
	KeyAccount           = "account"
	KeyAccountNext       = "account_next"
	KeyAccountPrev       = "account_prev"
	KeyToggleRead        = "toggle_read"
	KeyToggleFlag        = "toggle_flag"
	KeyDelete            = "delete"
	KeyFilter            = "filter"
	KeyVisualRange       = "visual_range"
	KeyNextThreadMessage = "next_thread_message"
	KeyPrevThreadMessage = "prev_thread_message"
	KeyCompose           = "compose"
	KeyReply             = "reply"
	KeyReplyAll          = "reply_all"
	KeyForward           = "forward"
	KeyArchive           = "archive"
	KeyJunk              = "junk"
	KeyMove              = "move"
	KeyEmptyTrash        = "empty_trash"
	KeyCollapse          = "collapse"
	KeySearch            = "search"
	KeySettings          = "settings"
	KeyHelp              = "help"
)

// keyDefaults is the shipped keymap. An action may have more than one key; the footer
// advertises the shortest one, while the help screen lists them all.
var keyDefaults = map[string][]string{
	KeyQuit:              {"q"},
	KeyForceQuit:         {"ctrl+c"},
	KeyNextPanel:         {"tab"},
	KeyPrevPanel:         {"shift+tab"},
	KeyBack:              {"esc"},
	KeyUp:                {"up"},
	KeyDown:              {"down"},
	KeyTop:               {"home", "g"},
	KeyBottom:            {"end", "G"},
	KeyOpen:              {"enter"},
	KeyAttachments:       {"s"},
	KeyFoldersOverlay:    {"\\"},
	KeyReload:            {"ctrl+r"},
	KeyAccount:           {"a"},
	KeyAccountNext:       {"]"},
	KeyAccountPrev:       {"["},
	KeyToggleRead:        {"u"},
	KeyToggleFlag:        {"f"},
	KeyDelete:            {"d"},
	KeyFilter:            {"/"},
	KeyVisualRange:       {"v"},
	KeyNextThreadMessage: {"n"},
	KeyPrevThreadMessage: {"p"},
	KeyCompose:           {"c"},
	KeyReply:             {"r"},
	KeyReplyAll:          {"R"},
	KeyForward:           {"F"},
	KeyArchive:           {"A"},
	KeyJunk:              {"!"},
	KeyMove:              {"m"},
	KeyEmptyTrash:        {"X"},
	KeyCollapse:          {"z"},
	KeySearch:            {"S"},
	KeySettings:          {","},
	KeyHelp:              {"?"},
}

// KeyDefaults returns a copy of the shipped keymap, so a caller cannot mutate it.
func KeyDefaults() map[string][]string {
	defaults := make(map[string][]string, len(keyDefaults))
	for name, keys := range keyDefaults {
		defaults[name] = append([]string(nil), keys...)
	}

	return defaults
}

// KeyActionNames returns every action [keys] may override, sorted, so validation errors
// and documentation can rely on a stable order.
func KeyActionNames() []string {
	names := make([]string, 0, len(keyDefaults))
	for name := range keyDefaults {
		names = append(names, name)
	}
	sort.Strings(names)

	return names
}

// KeyBindings is the effective keymap: the shipped defaults with [keys] applied. The
// result is a fresh map of fresh slices, so the model cannot write through to the
// config's defaults.
func (c *Config) KeyBindings() map[string][]string {
	effective := KeyDefaults()

	for name, keys := range c.Keys {
		if _, known := keyDefaults[name]; !known {
			continue
		}
		effective[name] = append([]string(nil), keys...)
	}

	return effective
}

// validateKeys checks the [keys] table against the shipped map and against itself.
//
// Overrides replace an action's keys rather than adding to them, and every rule is
// checked on the merged map: an override that takes a key the shipped map already uses
// is just as ambiguous as two overrides that share one, and resolving it in map order
// would be the silent first-wins this surface exists to prevent.
func (c *Config) validateKeys() error {
	names := make([]string, 0, len(c.Keys))
	for name := range c.Keys {
		names = append(names, name)
	}
	sort.Strings(names)

	normalized := make(map[string][]string, len(c.Keys))
	for _, name := range names {
		keys := c.Keys[name]

		if _, known := keyDefaults[name]; !known {
			return fmt.Errorf("config: [keys] unknown action %q", name)
		}
		if len(keys) == 0 {
			return fmt.Errorf("config: [keys] %q has no keys, so the action would be unreachable", name)
		}

		clean := make([]string, 0, len(keys))
		seen := make(map[string]bool, len(keys))

		for _, key := range keys {
			key = strings.TrimSpace(key)
			if key == "" {
				return fmt.Errorf("config: [keys] %q has an empty key", name)
			}
			if seen[key] {
				return fmt.Errorf("config: [keys] %q lists %q twice", name, key)
			}

			seen[key] = true
			clean = append(clean, key)
		}

		normalized[name] = clean
	}

	c.Keys = normalized

	// Keys are case-sensitive ("G" and "g" are different bindings), so the owner map
	// is keyed by the exact string the TUI resolves.
	effective := c.KeyBindings()
	owners := make(map[string]string, len(keyDefaults))

	for _, name := range KeyActionNames() {
		for _, key := range effective[name] {
			if other, taken := owners[key]; taken {
				return fmt.Errorf("config: [keys] %q is bound to both %q and %q", key, other, name)
			}

			owners[key] = name
		}
	}

	return nil
}
