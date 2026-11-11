// marmot overlay: GOROOT unique/clone.go adapted to marmot type descriptors
// (internal/rtabi) instead of gc's internal/abi layout.

// Copyright 2024 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package unique

import (
	"internal/abi"
	"internal/rtabi"
	"internal/stringslite"
	"unsafe"
)

// clone makes a copy of value, and may update string values found in value
// with a cloned version of those strings. The purpose of explicitly cloning
// strings is to avoid accidentally giving a large string a long lifetime.
//
// Note that this will clone strings in structs and arrays found in value,
// and will clone value if it itself is a string. It will not, however, clone
// strings if value is of interface or slice type (that is, found via an
// indirection).
func clone[T comparable](value T, seq *cloneSeq) T {
	for _, offset := range seq.stringOffsets {
		ps := (*string)(unsafe.Pointer(uintptr(unsafe.Pointer(&value)) + offset))
		*ps = stringslite.Clone(*ps)
	}
	return abi.EscapeToResultNonString(value)
}

// singleStringClone describes how to clone a single string.
var singleStringClone = cloneSeq{stringOffsets: []uintptr{0}}

// cloneSeq describes how to clone a value of a particular type.
type cloneSeq struct {
	stringOffsets []uintptr
}

// makeCloneSeq creates a cloneSeq for a type.
func makeCloneSeq(typ *rtabi.Type) cloneSeq {
	if typ == nil {
		return cloneSeq{}
	}
	if typ.Kind() == rtabi.String {
		return singleStringClone
	}
	var seq cloneSeq
	switch typ.Kind() {
	case rtabi.Struct:
		buildStructCloneSeq(typ, &seq, 0)
	case rtabi.Array:
		buildArrayCloneSeq(typ, &seq, 0)
	}
	return seq
}

// buildStructCloneSeq populates a cloneSeq for a struct type.
func buildStructCloneSeq(typ *rtabi.Type, seq *cloneSeq, baseOffset uintptr) {
	styp := (*rtabi.StructType)(unsafe.Pointer(typ))
	for i := 0; i < int(styp.NFields); i++ {
		f := styp.Field(i)
		switch f.Typ.Kind() {
		case rtabi.String:
			seq.stringOffsets = append(seq.stringOffsets, baseOffset+f.Offset())
		case rtabi.Struct:
			buildStructCloneSeq(f.Typ, seq, baseOffset+f.Offset())
		case rtabi.Array:
			buildArrayCloneSeq(f.Typ, seq, baseOffset+f.Offset())
		}
	}
}

// buildArrayCloneSeq populates a cloneSeq for an array type.
func buildArrayCloneSeq(typ *rtabi.Type, seq *cloneSeq, baseOffset uintptr) {
	atyp := (*rtabi.ArrayType)(unsafe.Pointer(typ))
	etyp := atyp.Elem
	offset := baseOffset
	for range atyp.Len {
		switch etyp.Kind() {
		case rtabi.String:
			seq.stringOffsets = append(seq.stringOffsets, offset)
		case rtabi.Struct:
			buildStructCloneSeq(etyp, seq, offset)
		case rtabi.Array:
			buildArrayCloneSeq(etyp, seq, offset)
		}
		offset += etyp.Size()
		align := uintptr(etyp.FieldAlign)
		offset = (offset + align - 1) &^ (align - 1)
	}
}
