package mt5legacy

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"net"
	"strings"
	"unicode/utf16"
)

// PipePrefix is the named pipe prefix the terminal listens on.
const PipePrefix = `\\.\pipe\MT5.Terminal.`

// PipeNameForPath derives the terminal's pipe name from the final path of
// terminal64.exe, as returned by GetFinalPathNameByHandleW, e.g.
// `\\?\C:\Program Files\MetaTrader 5\terminal64.exe`. The name is
// SHA-256 over the lowercased UTF-16LE path, upper-case hex.
func PipeNameForPath(finalPath string) string {
	u := utf16.Encode([]rune(strings.ToLower(finalPath)))
	b := make([]byte, 0, 2*len(u))
	for _, c := range u {
		b = binary.LittleEndian.AppendUint16(b, c)
	}
	sum := sha256.Sum256(b)
	return PipePrefix + strings.ToUpper(hex.EncodeToString(sum[:]))
}

// DialTCP connects to an mt5relay that forwards to a terminal pipe, which is
// how a non-Windows host reaches a terminal running on Windows or under Wine.
func DialTCP(ctx context.Context, addr string) (*Client, error) {
	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, err
	}
	return NewClient(ctx, conn)
}
