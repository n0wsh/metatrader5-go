// Package mt5legacy implements the MetaTrader 5 terminal IPC protocol.
// Like the MetaTrader5 Python package, it does not talk to
// the broker directly: it is an IPC client for a running terminal64.exe,
// which does the broker connection itself.
//
// Wire protocol (recovered from MetaTrader5 5.0.6231, _core.pyd):
//
//	request:  u32 len | u32 cmd | payload...            (len excludes itself)
//	response: u32 len | u32 cmd | i32 retcode | payload...
//
// All integers are little-endian, strings are UTF-16LE. Variable strings are
// sent as u32 char count followed by the chars (no terminator); strings in
// records are fixed wchar[32], NUL-padded. Records are packed (no alignment).
package mt5legacy

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"unicode/utf16"
)

// Command identifiers used on the terminal pipe.
const (
	cmdHello                   uint32 = 0x04
	cmdLogin                   uint32 = 0x05
	cmdAuthState               uint32 = 0x6d
	cmdHistoryOrdersByRange    uint32 = 0x8d
	cmdHistoryOrdersByPosition uint32 = 0x8e
	cmdHistoryOrdersByTicket   uint32 = 0x8f
	cmdHistoryDealsByRange     uint32 = 0x97
	cmdHistoryDealsByPosition  uint32 = 0x98
	cmdHistoryDealsByTicket    uint32 = 0x99
)

const (
	// helloVersion is the client API version the Python module announces.
	helloVersion uint32 = 3
	helloClient         = "Python"

	// maxMessage guards against a corrupt length prefix.
	maxMessage = 256 << 20
)

// Terminal authorization states returned by cmdAuthState.
const (
	authStateAuthorized uint32 = 2
	authStateFailed     uint32 = 3
)

type encoder struct{ buf bytes.Buffer }

func (e *encoder) u32(v uint32) { e.buf.Write(binary.LittleEndian.AppendUint32(nil, v)) }
func (e *encoder) u64(v uint64) { e.buf.Write(binary.LittleEndian.AppendUint64(nil, v)) }
func (e *encoder) i64(v int64)  { e.u64(uint64(v)) }

func (e *encoder) wstr(s string) {
	u := utf16.Encode([]rune(s))
	e.u32(uint32(len(u)))
	for _, c := range u {
		e.buf.Write(binary.LittleEndian.AppendUint16(nil, c))
	}
}

func newRequest(cmd uint32) *encoder {
	e := &encoder{}
	e.u32(cmd)
	return e
}

// frame prefixes the payload with its length.
func (e *encoder) frame() []byte {
	out := binary.LittleEndian.AppendUint32(make([]byte, 0, 4+e.buf.Len()), uint32(e.buf.Len()))
	return append(out, e.buf.Bytes()...)
}

// decoder reads little-endian fields from a response payload.
type decoder struct {
	b   []byte
	err error
}

func (d *decoder) take(n int) []byte {
	if d.err != nil {
		return nil
	}
	if len(d.b) < n {
		d.err = fmt.Errorf("mt5: short response: need %d bytes, have %d", n, len(d.b))
		return nil
	}
	p := d.b[:n]
	d.b = d.b[n:]
	return p
}

func (d *decoder) u32() uint32 {
	if p := d.take(4); p != nil {
		return binary.LittleEndian.Uint32(p)
	}
	return 0
}

func (d *decoder) u64() uint64 {
	if p := d.take(8); p != nil {
		return binary.LittleEndian.Uint64(p)
	}
	return 0
}

func (d *decoder) i32() int32   { return int32(d.u32()) }
func (d *decoder) i64() int64   { return int64(d.u64()) }
func (d *decoder) f64() float64 { return math.Float64frombits(d.u64()) }

// wfixed decodes a NUL-padded wchar[n] field.
func (d *decoder) wfixed(n int) string {
	p := d.take(2 * n)
	if p == nil {
		return ""
	}
	u := make([]uint16, 0, n)
	for i := 0; i < n; i++ {
		c := binary.LittleEndian.Uint16(p[2*i:])
		if c == 0 {
			break
		}
		u = append(u, c)
	}
	return string(utf16.Decode(u))
}

func writeRequest(w io.Writer, e *encoder) error {
	_, err := w.Write(e.frame())
	return err
}

// readResponse reads one framed message and returns its command id, retcode
// and the remaining payload.
func readResponse(r io.Reader) (cmd uint32, ret int32, body *decoder, err error) {
	var hdr [4]byte
	if _, err = io.ReadFull(r, hdr[:]); err != nil {
		return 0, 0, nil, err
	}
	n := binary.LittleEndian.Uint32(hdr[:])
	if n < 8 || n > maxMessage {
		return 0, 0, nil, fmt.Errorf("mt5: bad message length %d", n)
	}
	buf := make([]byte, n)
	if _, err = io.ReadFull(r, buf); err != nil {
		return 0, 0, nil, err
	}
	d := &decoder{b: buf}
	cmd = d.u32()
	ret = d.i32()
	return cmd, ret, d, nil
}
