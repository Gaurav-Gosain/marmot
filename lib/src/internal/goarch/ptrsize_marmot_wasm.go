package goarch

// PtrSize is the size of a pointer in bytes, as an ideal constant: 4 on
// wasm under marmot (32-bit memory; int and uintptr stay 8 bytes).
const PtrSize = 4
