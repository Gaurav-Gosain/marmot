// Package mlir has small helpers for writing MLIR text in the llvm dialect.
// It does no parsing and no validation: the backend (or mlir-translate)
// verifies the result.
package mlir

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// Common types, spelled as they appear inside and outside llvm dialect types.
const (
	Ptr  = "!llvm.ptr"
	I1   = "i1"
	I8   = "i8"
	I16  = "i16"
	I32  = "i32"
	I64  = "i64"
	F32  = "f32"
	F64  = "f64"
	Void = ""
)

// Struct returns a literal struct type.
func Struct(fields ...string) string {
	var b strings.Builder
	b.WriteString("!llvm.struct<(")
	for i, f := range fields {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(Inner(f))
	}
	b.WriteString(")>")
	return b.String()
}

// Array returns an array type.
func Array(n int64, elem string) string {
	return fmt.Sprintf("!llvm.array<%d x %s>", n, Inner(elem))
}

// Inner returns the spelling of t inside another llvm dialect type.
// MLIR accepts both forms there, the short one keeps text small.
func Inner(t string) string {
	if t == Ptr {
		return "ptr"
	}
	return t
}

// Symbol quotes a symbol name for use after '@'.
func Symbol(name string) string {
	if isBareIdent(name) {
		return "@" + name
	}
	return "@" + Quote(name)
}

func isBareIdent(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c == '_':
		case c >= '0' && c <= '9', c == '$', c == '.':
			if i == 0 {
				return false
			}
		default:
			return false
		}
	}
	return true
}

// Quote returns an MLIR string literal for arbitrary bytes.
func Quote(s string) string {
	var b strings.Builder
	b.Grow(len(s) + 2)
	b.WriteByte('"')
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 0x20 && c < 0x7f && c != '"' && c != '\\' {
			b.WriteByte(c)
			continue
		}
		const hex = "0123456789ABCDEF"
		b.WriteByte('\\')
		b.WriteByte(hex[c>>4])
		b.WriteByte(hex[c&15])
	}
	b.WriteByte('"')
	return b.String()
}

// IntAttr formats an integer constant attribute of the given bit width.
// v holds the bit pattern; it is printed as a signed value of that width.
func IntAttr(v uint64, bits int) string {
	if bits == 1 {
		if v&1 != 0 {
			return "true"
		}
		return "false"
	}
	if bits < 64 {
		shift := 64 - bits
		v = uint64(int64(v<<shift) >> shift)
	}
	return strconv.FormatInt(int64(v), 10)
}

// FloatAttr formats a float constant as an exact hexadecimal bit pattern.
func FloatAttr(v float64, typ string) string {
	if typ == F32 {
		return fmt.Sprintf("0x%08X", math.Float32bits(float32(v)))
	}
	return fmt.Sprintf("0x%016X", math.Float64bits(v))
}
