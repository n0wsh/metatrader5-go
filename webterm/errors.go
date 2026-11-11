package webterm

import (
	"errors"
	"fmt"
)

// ErrClosed indicates that Close was called on the client.
var ErrClosed = errors.New("webterm: client closed")

// StatusError reports a command rejected by the terminal. Use errors.As to
// inspect the numeric wire command and status without parsing error messages.
type StatusError struct {
	Command uint16
	Code    uint8
}

func (e *StatusError) Error() string {
	if e.Command == cmdLogin {
		switch e.Code {
		case 3:
			return "webterm: login failed: the server rejected the credentials"
		case 1:
			return "webterm: login failed: the account is not authorized"
		}
	}
	return fmt.Sprintf("webterm: command %d failed with status %d", e.Command, e.Code)
}

func loginError(code uint8) error {
	return &StatusError{Command: cmdLogin, Code: code}
}
