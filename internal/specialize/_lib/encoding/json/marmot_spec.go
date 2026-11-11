// Entry points for marmot's compile time JSON specialization (internal/specialize).
// The compiler generates a per type encoder for json.Marshal calls on statically
// known types and calls these functions for the leaves, so strings and numbers are
// written by the same code the general path uses. User code must not call them.

package json

import (
	"bytes"
	"math"
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
