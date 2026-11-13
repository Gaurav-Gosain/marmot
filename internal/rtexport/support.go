// Copyright 2018 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE-go file.

// Package rtexport writes export data like golang.org/x/tools/go/gcexportdata
// (whose writer it copies) but with unexported declarations too, so that a
// package code generation looks into by name (runtime) can load from cached
// export data. gcexportdata.Read reads it.
package rtexport

import "go/types"

// From x/tools internal/gcimporter (gcimporter.go, iimport.go): what the
// writer needs of the rest of that package.

const (
	debug = false
	trace = false
)

const (
	iexportVersionGo1_11         = 0
	iexportVersionPosCol         = 1
	iexportVersionGo1_18         = 2
	iexportVersionGenerics       = 2
	iexportVersionGenericMethods = 3
	iexportVersionRationalFloats = 4
	iexportVersion               = iexportVersionRationalFloats
)

const predeclReserved = 32

type itag uint64

const (
	definedType itag = iota
	pointerType
	sliceType
	arrayType
	chanType
	mapType
	signatureType
	structType
	interfaceType
	typeParamType
	instanceType
	unionType
	aliasType
)

var _ = types.Typ

// Object tags
const (
	varTag          = 'V'
	funcTag         = 'F'
	genericFuncTag  = 'G'
	constTag        = 'C'
	aliasTag        = 'A'
	genericAliasTag = 'B'
	typeParamTag    = 'P'
	typeTag         = 'T'
	genericTypeTag  = 'U'
)

const deltaNewFile = -64 // see cmd/compile/internal/gc/bexport.go
// Tags for the encoding of float constant values
// (iexportVersionRationalFloats and later); see (*exportWriter).float.
const (
	floatTagInt   = iota // integer-valued big.Rat: mpint (untyped sizing)
	floatTagRat          // other big.Rat: numerator and denominator, each a bigInt
	floatTagFloat        // big.Float: mpfloat

	// untypedMaxBytes is the maximum magnitude size of an mpint
	// encoded with untyped sizing; see [intSize].
	untypedMaxBytes = 64
)

func intSize(b *types.Basic) (signed bool, maxBytes uint) {
	if (b.Info() & types.IsUntyped) != 0 {
		return true, untypedMaxBytes
	}

	switch b.Kind() {
	case types.Float32, types.Complex64:
		return true, 3
	case types.Float64, types.Complex128:
		return true, 7
	}

	signed = (b.Info() & types.IsUnsigned) == 0
	switch b.Kind() {
	case types.Int8, types.Uint8:
		maxBytes = 1
	case types.Int16, types.Uint16:
		maxBytes = 2
	case types.Int32, types.Uint32:
		maxBytes = 4
	default:
		maxBytes = 8
	}

	return
}
