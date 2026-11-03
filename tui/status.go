package tui

import (
	"errors"
	"net"

	"github.com/renderorange/chute/mail"
)

// statusText is the whitelist of failures the status bar shortens. Anything the list
// does not name keeps its wrapped text, because an unanticipated failure is exactly the
// case where the detail matters most (GH-23 ruling).
//
// The mail layer keeps its account/host/stage wrapping for logs and tests; the wording
// lives here, next to the interface, the same split as "already in Trash".
func statusText(err error) string {
	if err == nil {
		return ""
	}

	// Timeout first: a dial that timed out is also a connect failure, and the timeout
	// is the more useful of the two to a person waiting on it.
	var timeout net.Error
	switch {
	case errors.As(err, &timeout) && timeout.Timeout():
		return "server timed out"

	case errors.Is(err, mail.ErrAuth):
		return "login rejected: check the password"

	case errors.Is(err, mail.ErrConnect):
		return "cannot connect to the mail server"

	case errors.Is(err, mail.ErrNoMoveSupport):
		return "server cannot move mail"
	}

	return err.Error()
}
