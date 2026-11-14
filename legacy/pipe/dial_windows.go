//go:build windows

package mt5legacy

import (
	"context"
	"errors"
	"net"
	"os/exec"
	"time"

	"github.com/Microsoft/go-winio"
	"golang.org/x/sys/windows"
)

// LaunchTimeout matches how long the Python package waits for a freshly
// started terminal to open its pipe.
const LaunchTimeout = 60 * time.Second

// TerminalOptions controls DialTerminal.
type TerminalOptions struct {
	// Launch starts terminal64.exe if its pipe is not available.
	Launch bool
	// Portable starts the terminal with /portable (data folder next to the exe).
	Portable bool
}

// TerminalPipeName resolves exePath (path to terminal64.exe) and returns the
// pipe that terminal listens on.
func TerminalPipeName(exePath string) (string, error) {
	p, err := windows.UTF16PtrFromString(exePath)
	if err != nil {
		return "", err
	}
	h, err := windows.CreateFile(p, windows.GENERIC_READ,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, nil, windows.OPEN_EXISTING, 0, 0)
	if err != nil {
		return "", err
	}
	defer func() { _ = windows.CloseHandle(h) }()
	buf := make([]uint16, windows.MAX_LONG_PATH)
	n, err := windows.GetFinalPathNameByHandle(h, &buf[0], uint32(len(buf)), 0)
	if err != nil {
		return "", err
	}
	return PipeNameForPath(windows.UTF16ToString(buf[:n])), nil
}

// DialPipe connects to a terminal by pipe name.
func DialPipe(ctx context.Context, pipe string) (*Client, error) {
	conn, err := DialPipeConn(ctx, pipe)
	if err != nil {
		return nil, err
	}
	return NewClient(ctx, conn)
}

// DialPipeConn opens the raw pipe without the API handshake (used by mt5relay).
func DialPipeConn(ctx context.Context, pipe string) (net.Conn, error) {
	return winio.DialPipeContext(ctx, pipe)
}

// DialTerminal connects to the terminal at exePath, optionally launching it.
func DialTerminal(ctx context.Context, exePath string, opts TerminalOptions) (*Client, error) {
	pipe, err := TerminalPipeName(exePath)
	if err != nil {
		return nil, err
	}
	if !opts.Launch {
		return DialPipe(ctx, pipe)
	}
	quick, cancel := context.WithTimeout(ctx, time.Second)
	c, err := DialPipe(quick, pipe)
	cancel()
	if err == nil {
		return c, nil
	}

	args := []string{}
	if opts.Portable {
		args = append(args, "/portable")
	}
	cmd := exec.Command(exePath, args...)
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	// Reap the detached terminal; connection attempts below report startup failures.
	go func() { _ = cmd.Wait() }()

	ctx, cancel = context.WithTimeout(ctx, LaunchTimeout)
	defer cancel()
	for {
		attempt, stop := context.WithTimeout(ctx, 2*time.Second)
		c, err = DialPipe(attempt, pipe)
		stop()
		if err == nil {
			return c, nil
		}
		select {
		case <-ctx.Done():
			return nil, errors.Join(resError(ResInternalFailTimeout), err)
		case <-time.After(500 * time.Millisecond):
		}
	}
}
