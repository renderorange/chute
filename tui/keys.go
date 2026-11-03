package tui

import "github.com/renderorange/chute/config"

// action is one user intent, independent of the key that triggers it. Every
// binding goes through this map so M6 can load a [keys] table from config
// without touching a handler.
type action int

const (
	actionNone action = iota
	actionQuit
	actionForceQuit
	actionNextPanel
	actionPrevPanel
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
	actionFilter
	actionVisualRange
	actionNextThreadMessage
	actionPrevThreadMessage
	actionCompose
	actionReply
	actionReplyAll
	actionForward
	actionArchive
	actionJunk
	actionHelp
)

// actionNames maps each intent to its name in config's [keys] table. The names are the
// stable identifiers: handlers dispatch on the action, and config validates the name.
var actionNames = map[action]string{
	actionQuit:              config.KeyQuit,
	actionForceQuit:         config.KeyForceQuit,
	actionNextPanel:         config.KeyNextPanel,
	actionPrevPanel:         config.KeyPrevPanel,
	actionBack:              config.KeyBack,
	actionUp:                config.KeyUp,
	actionDown:              config.KeyDown,
	actionTop:               config.KeyTop,
	actionBottom:            config.KeyBottom,
	actionOpen:              config.KeyOpen,
	actionAttachments:       config.KeyAttachments,
	actionFoldersOverlay:    config.KeyFoldersOverlay,
	actionReload:            config.KeyReload,
	actionAccount:           config.KeyAccount,
	actionToggleRead:        config.KeyToggleRead,
	actionToggleFlag:        config.KeyToggleFlag,
	actionDelete:            config.KeyDelete,
	actionFilter:            config.KeyFilter,
	actionVisualRange:       config.KeyVisualRange,
	actionNextThreadMessage: config.KeyNextThreadMessage,
	actionPrevThreadMessage: config.KeyPrevThreadMessage,
	actionCompose:           config.KeyCompose,
	actionReply:             config.KeyReply,
	actionReplyAll:          config.KeyReplyAll,
	actionForward:           config.KeyForward,
	actionArchive:           config.KeyArchive,
	actionJunk:              config.KeyJunk,
	actionHelp:              config.KeyHelp,
}

// bindings maps each intent to the keys that trigger it in any mode.
type bindings map[action][]string

// defaultBindings is the shipped key layout. Modal pickers reuse the same
// intents: actionOpen confirms, actionBack cancels. actionQuit leaves the app
// from the main view but only closes a modal; actionForceQuit always leaves.
//
// The keys themselves live in config.KeyDefaults, because [keys] validation has to
// see them to reject an override that steals a default key.
func defaultBindings() bindings {
	return bindingsFromConfig(config.KeyDefaults())
}

// bindingsFromConfig turns an action-name keymap into the action map the handlers use.
// A name with no action is ignored here: config.Load has already rejected it, and this
// keeps a hand-built test config from inventing actions.
func bindingsFromConfig(keys map[string][]string) bindings {
	b := make(bindings, len(actionNames))

	for act, name := range actionNames {
		if bound, ok := keys[name]; ok {
			b[act] = bound
		}
	}

	return b
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
