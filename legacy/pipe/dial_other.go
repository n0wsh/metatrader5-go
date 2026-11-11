//go:build !windows

package mt5legacy

import (
	"context"
	"errors"
)

// ErrPipeUnsupported is returned by the pipe dialers off Windows. Use DialTCP
// with an mt5relay running next to the terminal instead.
var ErrPipeUnsupported = errors.New("mt5: terminal named pipes are only reachable on Windows; use DialTCP via mt5relay")

// TerminalOptions controls DialTerminal.
type TerminalOptions struct {
	Launch   bool
	Portable bool
}

func DialPipe(context.Context, string) (*Client, error) { return nil, ErrPipeUnsupported }

func DialTerminal(context.Context, string, TerminalOptions) (*Client, error) {
	return nil, ErrPipeUnsupported
}
