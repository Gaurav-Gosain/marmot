// Entry points for marmot's compile time JSON specialization (internal/specialize).
// The compiler generates a per type encoder for json.Marshal calls on statically
// known types and calls these functions for the leaves, so strings and numbers are
// written by the same code the general path uses. User code must not call them.

package json

import (
	"bytes"
	"encoding/binary"
	"math"
	"math/bits"
	"strconv"
	"strings"
	"sync"
	"unsafe"

	"encoding/json/internal/jsonflags"
	"encoding/json/internal/jsonwire"
)

var marmotV1Flags = jsonflags.Flags{
	Presence: uint64(jsonflags.DefaultV1Flags),
	Values:   uint64(jsonflags.DefaultV1Flags),
}

// MarmotAppendString appends s as a JSON string with v1 escaping.
func MarmotAppendString(b []byte, s string) []byte {
	b, _ = jsonwire.AppendQuote(b, unsafe.Slice(unsafe.StringData(s), len(s)), &marmotV1Flags)
	return b
}

// MarmotAppendFloat appends f as v1 does; ok is false for NaN and infinities,
// which Marshal reports as errors.
func MarmotAppendFloat(b []byte, f float64, bits int) ([]byte, bool) {
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return b, false
	}
	return jsonwire.AppendFloat(b, f, bits), true
}

var marmotBufs = sync.Pool{New: func() any { b := make([]byte, 0, 1024); return &b }}

// MarmotGetBuf returns an empty scratch buffer for an encoder.
func MarmotGetBuf() *[]byte {
	p := marmotBufs.Get().(*[]byte)
	*p = (*p)[:0]
	return p
}

// MarmotPutBuf returns the scratch buffer holding b and returns the result
// of Marshal: a copy of b, made the way Marshal makes it.
func MarmotPutBuf(p *[]byte, b []byte) []byte {
	out := bytes.Clone(b)
	if cap(b) <= 1<<20 {
		*p = b[:0]
		marmotBufs.Put(p)
	}
	return out
}

// MarmotDropBuf returns the scratch buffer after a failed encoding.
func MarmotDropBuf(p *[]byte, b []byte) {
	if cap(b) <= 1<<20 {
		*p = b[:0]
		marmotBufs.Put(p)
	}
}

// MarmotDecoder is the scanner of generated decoders. A generated decoder
// runs twice: first dry (it validates syntax and that every value fits the
// Go type, and writes nothing), then for real. Any method returning false
// sends the call back to Unmarshal, which then sees an untouched value.
type MarmotDecoder struct {
	buf   []byte
	pos   int
	depth int
	cache *[256]string // interned short strings, as the general path keeps
}

var marmotCaches = sync.Pool{New: func() any { return new([256]string) }}

func MarmotNewDecoder(data []byte) MarmotDecoder { return MarmotDecoder{buf: data} }

// MarmotNewDecoderCached is MarmotNewDecoder with a string cache; call
// Release when done.
func MarmotNewDecoderCached(data []byte) MarmotDecoder {
	return MarmotDecoder{buf: data, cache: marmotCaches.Get().(*[256]string)}
}

// Release returns the string cache.
func (d *MarmotDecoder) Release() {
	if d.cache != nil {
		marmotCaches.Put(d.cache)
		d.cache = nil
	}
}

// intern is encoding/json/v2's makeString.
func (d *MarmotDecoder) intern(b []byte) string {
	c := d.cache
	if c == nil || len(b) < 2 || len(b) > 256 {
		return string(b)
	}
	var h uint32
	switch {
	case len(b) >= 8:
		lo := binary.LittleEndian.Uint64(b[:8])
		hi := binary.LittleEndian.Uint64(b[len(b)-8:])
		h = marmotHash64(uint32(lo), uint32(lo>>32)) ^ marmotHash64(uint32(hi), uint32(hi>>32))
	case len(b) >= 4:
		lo := binary.LittleEndian.Uint32(b[:4])
		hi := binary.LittleEndian.Uint32(b[len(b)-4:])
		h = marmotHash64(lo, hi)
	default:
		lo := binary.LittleEndian.Uint16(b[:2])
		hi := binary.LittleEndian.Uint16(b[len(b)-2:])
		h = marmotHash64(uint32(lo), uint32(hi))
	}
	i := h % uint32(len(*c))
	if s := (*c)[i]; s == string(b) {
		return s
	}
	s := string(b)
	(*c)[i] = s
	return s
}

func marmotHash64(lo, hi uint32) uint32 {
	h := uint32(0x165667b1) + 8
	h += lo * 0xc2b2ae3d
	h = bits.RotateLeft32(h, 17) * 0x27d4eb2f
	h += hi * 0xc2b2ae3d
	h = bits.RotateLeft32(h, 17) * 0x27d4eb2f
	return h
}

func (d *MarmotDecoder) ws() {
	for d.pos < len(d.buf) {
		switch d.buf[d.pos] {
		case ' ', '\t', '\r', '\n':
			d.pos++
		default:
			return
		}
	}
}

// Peek returns the first byte of the next token, or 0 at the end.
func (d *MarmotDecoder) Peek() byte {
	d.ws()
	if d.pos < len(d.buf) {
		return d.buf[d.pos]
	}
	return 0
}

// Lit consumes the literal s (null, true, false).
func (d *MarmotDecoder) Lit(s string) bool {
	d.ws()
	if len(d.buf)-d.pos >= len(s) && string(d.buf[d.pos:d.pos+len(s)]) == s {
		d.pos += len(s)
		return true
	}
	return false
}

// Open consumes the opening delimiter c ('{' or '[').
func (d *MarmotDecoder) Open(c byte) bool {
	d.ws()
	if d.pos < len(d.buf) && d.buf[d.pos] == c && d.depth < 10000 {
		d.pos++
		d.depth++
		return true
	}
	return false
}

// Next advances to member or element i of the object or array closed by c.
// It reports false for more when the closing delimiter was consumed.
func (d *MarmotDecoder) Next(c byte, i int) (more, ok bool) {
	d.ws()
	if d.pos >= len(d.buf) {
		return false, false
	}
	switch d.buf[d.pos] {
	case c:
		d.pos++
		d.depth--
		return false, true
	case ',':
		if i == 0 {
			return false, false
		}
		d.pos++
		return true, true
	}
	return true, i == 0
}

// Bytes reads a string token and returns its unquoted bytes, which may alias
// the input or a fresh buffer.
func (d *MarmotDecoder) Bytes() ([]byte, bool) {
	d.ws()
	if d.pos >= len(d.buf) || d.buf[d.pos] != '"' {
		return nil, false
	}
	for i := d.pos + 1; i < len(d.buf); i++ {
		c := d.buf[i]
		if c == '"' {
			s := d.buf[d.pos+1 : i]
			d.pos = i + 1
			return s, true
		}
		if c < 0x20 || c == '\\' || c >= 0x80 {
			break
		}
	}
	var flags jsonwire.ValueFlags
	n, err := jsonwire.ConsumeString(&flags, d.buf[d.pos:], !marmotV1Flags.Get(jsonflags.AllowInvalidUTF8))
	if err != nil {
		return nil, false
	}
	val := jsonwire.UnquoteMayCopy(d.buf[d.pos:d.pos+n], flags.IsVerbatim())
	d.pos += n
	return val, true
}

// Key reads an object member name and the colon after it.
func (d *MarmotDecoder) Key() ([]byte, bool) {
	k, ok := d.Bytes()
	if !ok {
		return nil, false
	}
	d.ws()
	if d.pos >= len(d.buf) || d.buf[d.pos] != ':' {
		return nil, false
	}
	d.pos++
	return k, true
}

// Str reads a string token.
func (d *MarmotDecoder) Str() (string, bool) {
	b, ok := d.Bytes()
	if !ok {
		return "", false
	}
	return d.intern(b), true
}

func (d *MarmotDecoder) number() ([]byte, bool) {
	d.ws()
	if d.pos >= len(d.buf) {
		return nil, false
	}
	if c := d.buf[d.pos]; c != '-' && (c < '0' || c > '9') {
		return nil, false
	}
	n, err := jsonwire.ConsumeNumber(d.buf[d.pos:])
	if err != nil || n == 0 {
		return nil, false
	}
	v := d.buf[d.pos : d.pos+n]
	d.pos += n
	return v, true
}

// Int reads a JSON number into a signed integer of the given size, as the
// general path does; it fails where that path reports an error.
func (d *MarmotDecoder) Int(bits int) (int64, bool) {
	val, ok := d.number()
	if !ok {
		return 0, false
	}
	neg := len(val) > 0 && val[0] == '-'
	if neg {
		val = val[1:]
	}
	n, ok := jsonwire.ParseUint(val)
	maxInt := uint64(1) << (bits - 1)
	if !ok || (neg && n > maxInt) || (!neg && n > maxInt-1) {
		return 0, false
	}
	if neg {
		return int64(-n), true
	}
	return int64(n), true
}

// Uint reads a JSON number into an unsigned integer of the given size.
func (d *MarmotDecoder) Uint(bits int) (uint64, bool) {
	val, ok := d.number()
	if !ok {
		return 0, false
	}
	n, ok := jsonwire.ParseUint(val)
	maxUint := uint64(1) << bits
	if !ok || (bits < 64 && n > maxUint-1) {
		return 0, false
	}
	return n, true
}

// Float reads a JSON number into a float of the given size.
func (d *MarmotDecoder) Float(bits int) (float64, bool) {
	val, ok := d.number()
	if !ok {
		return 0, false
	}
	f, err := strconv.ParseFloat(string(val), bits)
	if err != nil {
		return 0, false
	}
	return f, true
}

// Bool reads true or false.
func (d *MarmotDecoder) Bool() (bool, bool) {
	switch d.Peek() {
	case 't':
		return true, d.Lit("true")
	case 'f':
		return false, d.Lit("false")
	}
	return false, false
}

// Skip validates and skips any JSON value (an unknown member's value).
func (d *MarmotDecoder) Skip() bool {
	switch d.Peek() {
	case '{':
		if !d.Open('{') {
			return false
		}
		for i := 0; ; i++ {
			more, ok := d.Next('}', i)
			if !ok {
				return false
			}
			if !more {
				return true
			}
			if _, ok := d.Key(); !ok || !d.Skip() {
				return false
			}
		}
	case '[':
		if !d.Open('[') {
			return false
		}
		for i := 0; ; i++ {
			more, ok := d.Next(']', i)
			if !ok {
				return false
			}
			if !more {
				return true
			}
			if !d.Skip() {
				return false
			}
		}
	case '"':
		_, ok := d.Bytes()
		return ok
	case 't':
		return d.Lit("true")
	case 'f':
		return d.Lit("false")
	case 'n':
		return d.Lit("null")
	}
	_, ok := d.number()
	return ok
}

// End reports whether only whitespace is left.
func (d *MarmotDecoder) End() bool {
	d.ws()
	return d.pos == len(d.buf)
}

// MarmotMaybeFold reports whether the unknown member name k could match one
// of names under the case-insensitive matching of the general path.
func MarmotMaybeFold(k []byte, names ...string) bool {
	for _, c := range k {
		if c >= 0x80 {
			return true
		}
	}
	for _, n := range names {
		if len(n) == len(k) && strings.EqualFold(n, string(k)) {
			return true
		}
	}
	return false
}

// KeyStr reads an object member name as a string, and the colon after it.
func (d *MarmotDecoder) KeyStr() (string, bool) {
	k, ok := d.Key()
	if !ok {
		return "", false
	}
	return d.intern(k), true
}
