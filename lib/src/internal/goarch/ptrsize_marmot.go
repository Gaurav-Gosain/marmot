//go:build !wasm

package goarch

// PtrSize is the size of a pointer in bytes, as an ideal constant.
const PtrSize = 4 << (^uintptr(0) >> 63)
