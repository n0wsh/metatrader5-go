package webterm

import (
	"encoding/binary"
	"fmt"
	"math"
	"unicode/utf16"
)

// Command ids used by the web terminal.
const (
	cmdBootstrap    uint16 = 0
	cmdLogout       uint16 = 2
	cmdAccount      uint16 = 3
	cmdPositions    uint16 = 4
	cmdTradeHistory uint16 = 5
	cmdLogin        uint16 = 28
	cmdInit         uint16 = 29
	cmdPing         uint16 = 51
)

// Frame sizes.
const (
	outerHeaderSize    = 8 // u32 body length + u32 version
	outerVersion       = 1
	commandHeaderSize  = 4 // 2 random bytes + u16 command
	responseHeaderSize = 5 // 2 random bytes + u16 command + u8 status
)

// propType is the wire tag of a serialized field.
type propType uint8

const (
	propInt8     propType = 1
	propInt16    propType = 2
	propInt32    propType = 3
	propUint8    propType = 4
	propUint16   propType = 5
	propUint32   propType = 6
	propFloat32  propType = 7
	propFloat64  propType = 8
	propFileTime propType = 9
	propChar     propType = 10
	propWchar    propType = 11
	propBuffer   propType = 12
	propInt64    propType = 17
	propUint64   propType = 18
)

// field describes one field of a record schema.
type field struct {
	typ    propType
	length int // propLength in bytes; 0 means "as needed"
}

// value is one field of a record to serialize.
type value struct {
	typ    propType
	length int
	num    uint64
	f      float64
	str    string
	buf    []byte
}

func u8(v uint8) value    { return value{typ: propUint8, num: uint64(v)} }
func i8(v int8) value     { return value{typ: propInt8, num: uint64(v)} }
func u16(v uint16) value  { return value{typ: propUint16, num: uint64(v)} }
func u32(v uint32) value  { return value{typ: propUint32, num: uint64(v)} }
func u64(v uint64) value  { return value{typ: propUint64, num: v} }
func i64(v int64) value   { return value{typ: propInt64, num: uint64(v)} }
func i32(v int32) value   { return value{typ: propInt32, num: uint64(uint32(v))} }
func f64(v float64) value { return value{typ: propFloat64, f: v} }
func bytesVal(b []byte, n int) value {
	return value{typ: propBuffer, length: n, buf: b}
}

// wchar serializes s as UTF-16LE padded to length bytes.
func wchar(s string, length int) value {
	return value{typ: propWchar, length: length, str: s}
}

// serialize lays the fields out the way the terminal's serializer does:
// fixed width integers and floats, UTF-16LE strings and raw buffers, all
// without alignment padding.
func serialize(fields []value) []byte {
	size := 0
	for _, f := range fields {
		size += f.size()
	}
	out := make([]byte, size)
	pos := 0
	for _, f := range fields {
		fieldStart := pos
		switch f.typ {
		case propInt8:
			out[pos] = byte(int8(f.num))
			pos++
		case propUint8:
			out[pos] = byte(f.num)
			pos++
		case propInt16:
			binary.LittleEndian.PutUint16(out[pos:], uint16(int16(f.num)))
			pos += 2
		case propUint16:
			binary.LittleEndian.PutUint16(out[pos:], uint16(f.num))
			pos += 2
		case propInt32:
			binary.LittleEndian.PutUint32(out[pos:], uint32(int32(f.num)))
			pos += 4
		case propUint32:
			binary.LittleEndian.PutUint32(out[pos:], uint32(f.num))
			pos += 4
		case propInt64:
			binary.LittleEndian.PutUint64(out[pos:], f.num)
			pos += 8
		case propUint64:
			binary.LittleEndian.PutUint64(out[pos:], f.num)
			pos += 8
		case propFloat32:
			binary.LittleEndian.PutUint32(out[pos:], math.Float32bits(float32(f.f)))
			pos += 4
		case propFloat64:
			binary.LittleEndian.PutUint64(out[pos:], math.Float64bits(f.f))
			pos += 8
		case propFileTime:
			ft := uint64((f.f + 11644473600000) * 10000)
			binary.LittleEndian.PutUint64(out[pos:], ft)
			pos += 8
		case propWchar:
			units := utf16.Encode([]rune(f.str))
			for i, u := range units {
				if 2*i+2 > f.length {
					break
				}
				binary.LittleEndian.PutUint16(out[pos:], u)
				pos += 2
			}
			// The rest of the field stays zero, the buffer is pre-zeroed.
			pos += f.length - (pos - fieldStart)
		case propChar:
			for i := 0; i < len(f.str) && i < f.length; i++ {
				out[pos+i] = f.str[i]
			}
			pos += f.length
		case propBuffer:
			n := copy(out[pos:], f.buf)
			pos += n
			if f.length > n {
				pos += f.length - n
			}
		default:
			panic(fmt.Sprintf("webterm: unsupported property type %d", f.typ))
		}
	}
	return out
}

// size returns the number of bytes the field occupies.
func (f value) size() int {
	switch f.typ {
	case propInt8, propUint8:
		return 1
	case propInt16, propUint16:
		return 2
	case propInt32, propUint32, propFloat32:
		return 4
	case propInt64, propUint64, propFloat64, propFileTime:
		return 8
	case propWchar:
		if f.length > 0 {
			return f.length
		}
		return 2 * len(utf16.Encode([]rune(f.str)))
	case propChar:
		if f.length > 0 {
			return f.length
		}
		return len(f.str)
	case propBuffer:
		if f.length > 0 {
			return f.length
		}
		return len(f.buf)
	}
	panic(fmt.Sprintf("webterm: unsupported property type %d", f.typ))
}

// schemaSize returns the number of bytes one record of the schema occupies.
func schemaSize(schema []field) int {
	size := 0
	for _, f := range schema {
		size += f.size()
	}
	return size
}

// parse decodes one record of the given schema at the given offset.
func parse(buf []byte, schema []field, offset int) ([]any, error) {
	out := make([]any, 0, len(schema))
	pos := offset
	for _, f := range schema {
		size := f.size()
		if pos+size > len(buf) {
			return nil, fmt.Errorf("webterm: truncated record at %d: need %d bytes, have %d", pos, size, len(buf)-pos)
		}
		raw := buf[pos : pos+size]
		pos += size
		switch f.typ {
		case propInt8:
			out = append(out, int8(raw[0]))
		case propUint8:
			out = append(out, raw[0])
		case propInt16:
			out = append(out, int16(binary.LittleEndian.Uint16(raw)))
		case propUint16:
			out = append(out, binary.LittleEndian.Uint16(raw))
		case propInt32:
			out = append(out, int32(binary.LittleEndian.Uint32(raw)))
		case propUint32:
			out = append(out, binary.LittleEndian.Uint32(raw))
		case propInt64:
			out = append(out, int64(binary.LittleEndian.Uint64(raw)))
		case propUint64:
			out = append(out, binary.LittleEndian.Uint64(raw))
		case propFloat32:
			out = append(out, math.Float32frombits(binary.LittleEndian.Uint32(raw)))
		case propFloat64:
			out = append(out, math.Float64frombits(binary.LittleEndian.Uint64(raw)))
		case propFileTime:
			ft := binary.LittleEndian.Uint64(raw)
			out = append(out, float64(ft/10000)-11644473600000)
		case propWchar:
			out = append(out, decodeWchar(raw))
		case propChar:
			out = append(out, string(trimNul(raw)))
		case propBuffer:
			b := make([]byte, len(raw))
			copy(b, raw)
			out = append(out, b)
		default:
			return nil, fmt.Errorf("webterm: unsupported property type %d", f.typ)
		}
	}
	return out, nil
}

// size returns the number of bytes the field occupies in a record.
func (f field) size() int {
	switch f.typ {
	case propInt8, propUint8:
		return 1
	case propInt16, propUint16:
		return 2
	case propInt32, propUint32, propFloat32:
		return 4
	case propInt64, propUint64, propFloat64, propFileTime:
		return 8
	case propWchar, propChar, propBuffer:
		return f.length
	}
	panic(fmt.Sprintf("webterm: unsupported property type %d", f.typ))
}

// decodeWchar reads a zero padded UTF-16LE string.
func decodeWchar(raw []byte) string {
	units := make([]uint16, 0, len(raw)/2)
	for i := 0; i+1 < len(raw); i += 2 {
		u := binary.LittleEndian.Uint16(raw[i:])
		if u == 0 {
			break
		}
		units = append(units, u)
	}
	return string(utf16.Decode(units))
}

func trimNul(raw []byte) []byte {
	for i, b := range raw {
		if b == 0 {
			return raw[:i]
		}
	}
	return raw
}
