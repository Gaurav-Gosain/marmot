// marmot overlay of internal/runtime/atomic.
//
// The body-less functions in this file are compiler intrinsics. The marmot
// compiler recognizes a call to any of them by package path
// ("internal/runtime/atomic") and name and emits the LLVM atomic operation
// inline, sequentially consistent:
//
//	Load8, Load, Load64          load atomic seq_cst (i8, i32, i64)
//	Store8, Store, Store64       store atomic seq_cst
//	Xadd, Xadd64                 atomicrmw add, returns the NEW value
//	Xchg8, Xchg, Xchg64          atomicrmw xchg, returns the old value
//	Cas, Cas64                   cmpxchg seq_cst seq_cst, returns success
//	And8, Or8, And, Or           atomicrmw and/or, no result
//	And32, Or32, And64, Or64     atomicrmw and/or, returns the OLD value
//	Loadp, StorepNoWB, Casp1     pointer load, store, cmpxchg (marmot has no
//	                             write barriers)
//
// Everything else in the package is ordinary Go built on these.
// The remaining files (types.go, types_64bit.go, unaligned.go, doc.go) are
// copied unchanged from GOROOT.

package atomic

import "unsafe"

func Load8(ptr *uint8) uint8
func Load(ptr *uint32) uint32
func Load64(ptr *uint64) uint64

func Store8(ptr *uint8, val uint8)
func Store(ptr *uint32, val uint32)
func Store64(ptr *uint64, val uint64)

func Xadd(ptr *uint32, delta int32) uint32
func Xadd64(ptr *uint64, delta int64) uint64

func Xchg8(ptr *uint8, new uint8) uint8
func Xchg(ptr *uint32, new uint32) uint32
func Xchg64(ptr *uint64, new uint64) uint64

func Cas(ptr *uint32, old, new uint32) bool
func Cas64(ptr *uint64, old, new uint64) bool

func And8(ptr *uint8, val uint8)
func Or8(ptr *uint8, val uint8)
func And(ptr *uint32, val uint32)
func Or(ptr *uint32, val uint32)
func And32(ptr *uint32, val uint32) uint32
func Or32(ptr *uint32, val uint32) uint32
func And64(ptr *uint64, val uint64) uint64
func Or64(ptr *uint64, val uint64) uint64

func Loadp(ptr unsafe.Pointer) unsafe.Pointer
func StorepNoWB(ptr unsafe.Pointer, val unsafe.Pointer)
func Casp1(ptr *unsafe.Pointer, old, new unsafe.Pointer) bool

// Derived operations. uintptr, int and pointers are 64 bit on every
// supported target.

func LoadAcq(ptr *uint32) uint32          { return Load(ptr) }
func LoadAcq64(ptr *uint64) uint64        { return Load64(ptr) }
func LoadAcquintptr(ptr *uintptr) uintptr { return uintptr(Load64((*uint64)(unsafe.Pointer(ptr)))) }
func Loaduintptr(ptr *uintptr) uintptr    { return uintptr(Load64((*uint64)(unsafe.Pointer(ptr)))) }
func Loaduint(ptr *uint) uint             { return uint(Load64((*uint64)(unsafe.Pointer(ptr)))) }
func Loadint32(ptr *int32) int32          { return int32(Load((*uint32)(unsafe.Pointer(ptr)))) }
func Loadint64(ptr *int64) int64          { return int64(Load64((*uint64)(unsafe.Pointer(ptr)))) }

func StoreRel(ptr *uint32, val uint32)   { Store(ptr, val) }
func StoreRel64(ptr *uint64, val uint64) { Store64(ptr, val) }
func StoreReluintptr(ptr *uintptr, val uintptr) {
	Store64((*uint64)(unsafe.Pointer(ptr)), uint64(val))
}
func Storeuintptr(ptr *uintptr, new uintptr) { Store64((*uint64)(unsafe.Pointer(ptr)), uint64(new)) }
func Storeint32(ptr *int32, new int32)       { Store((*uint32)(unsafe.Pointer(ptr)), uint32(new)) }
func Storeint64(ptr *int64, new int64)       { Store64((*uint64)(unsafe.Pointer(ptr)), uint64(new)) }

func Xadduintptr(ptr *uintptr, delta uintptr) uintptr {
	return uintptr(Xadd64((*uint64)(unsafe.Pointer(ptr)), int64(delta)))
}
func Xaddint32(ptr *int32, delta int32) int32 {
	return int32(Xadd((*uint32)(unsafe.Pointer(ptr)), delta))
}
func Xaddint64(ptr *int64, delta int64) int64 {
	return int64(Xadd64((*uint64)(unsafe.Pointer(ptr)), delta))
}

func Xchguintptr(ptr *uintptr, new uintptr) uintptr {
	return uintptr(Xchg64((*uint64)(unsafe.Pointer(ptr)), uint64(new)))
}
func Xchgint32(ptr *int32, new int32) int32 {
	return int32(Xchg((*uint32)(unsafe.Pointer(ptr)), uint32(new)))
}
func Xchgint64(ptr *int64, new int64) int64 {
	return int64(Xchg64((*uint64)(unsafe.Pointer(ptr)), uint64(new)))
}

func CasRel(ptr *uint32, old, new uint32) bool { return Cas(ptr, old, new) }
func Casint32(ptr *int32, old, new int32) bool {
	return Cas((*uint32)(unsafe.Pointer(ptr)), uint32(old), uint32(new))
}
func Casint64(ptr *int64, old, new int64) bool {
	return Cas64((*uint64)(unsafe.Pointer(ptr)), uint64(old), uint64(new))
}
func Casuintptr(ptr *uintptr, old, new uintptr) bool {
	return Cas64((*uint64)(unsafe.Pointer(ptr)), uint64(old), uint64(new))
}

func Anduintptr(ptr *uintptr, val uintptr) uintptr {
	return uintptr(And64((*uint64)(unsafe.Pointer(ptr)), uint64(val)))
}
func Oruintptr(ptr *uintptr, val uintptr) uintptr {
	return uintptr(Or64((*uint64)(unsafe.Pointer(ptr)), uint64(val)))
}
