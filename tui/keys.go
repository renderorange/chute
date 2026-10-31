package tui

// action is one user intent, independent of the key that triggers it. Every
// binding goes through this map so M6 can load a [keys] table from config
// without touching a handler.
type action int

const (
	actionNone action = iota
	actionQuit
	actionForceQuit
	actionNextPanel
	actionBack
	actionUp
	actionDown
	actionTop
	actionBottom
	actionOpen
	actionAttachments
	actionFoldersOverlay
	actionReload
	actionAccount
	actionToggleRead
	actionToggleFlag
	actionDelete
	actionVisualRange
)

// bindings maps each intent to the keys that trigger it in any mode.
type bindings map[action][]string

// defaultBindings is the shipped key layout. Modal pickers reuse the same
// intents: actionOpen confirms, actionBack cancels. actionQuit leaves the app
// from the main view but only closes a modal; actionForceQuit always leaves.
func defaultBindings() bindings {
	return bindings{
		actionQuit:           {"q"},
		actionForceQuit:      {"ctrl+c"},
		actionNextPanel:      {"tab"},
		actionBack:           {"esc"},
		actionUp:             {"up", "k"},
		actionDown:           {"down", "j"},
		actionTop:            {"home", "g"},
		actionBottom:         {"end", "G"},
		actionOpen:           {"enter"},
		actionAttachments:    {"s"},
		actionFoldersOverlay: {"\\"},
		actionReload:         {"r"},
		actionAccount:        {"a"},
		actionToggleRead:     {"m"},
		actionToggleFlag:     {"f"},
		actionDelete:         {"d"},
		actionVisualRange:    {"v"},
	}
}

// resolve returns the action bound to a key, or actionNone. No key may be bound
// twice, which TestDefaultBindingsAreUnique enforces, so map order is irrelevant.
func (b bindings) resolve(key string) action {
	for act, keys := range b {
		for _, bound := range keys {
			if bound == key {
				return act
			}
		}
	}

	return actionNone
}
