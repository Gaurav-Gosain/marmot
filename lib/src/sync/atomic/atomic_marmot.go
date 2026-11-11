// Package atomic provides low-level atomic memory primitives.
//
// marmot overlay: the functions of the stock doc.go and doc_64.go, which
// are assembly in the gc toolchain, implemented on internal/runtime/atomic,
// whose primitives the marmot compiler expands inline. type.go and value.go
// are the stock files. uintptr and pointers are 64 bit on every supported
// target.
package atomic

import (
	ra "internal/runtime/atomic"
	"unsafe"
)

func u32(p *int32) *uint32    { return (*uint32)(unsafe.Pointer(p)) }
func u64(p *int64) *uint64    { return (*uint64)(unsafe.Pointer(p)) }
func up64(p *uintptr) *uint64 { return (*uint64)(unsafe.Pointer(p)) }

func SwapInt32(addr *int32, new int32) (old int32)     { return int32(ra.Xchg(u32(addr), uint32(new))) }
func SwapUint32(addr *uint32, new uint32) (old uint32) { return ra.Xchg(addr, new) }
func SwapUintptr(addr *uintptr, new uintptr) (old uintptr) {
	return uintptr(ra.Xchg64(up64(addr), uint64(new)))
}
func SwapPointer(addr *unsafe.Pointer, new unsafe.Pointer) (old unsafe.Pointer) {
	for {
		old = ra.Loadp(unsafe.Pointer(addr))
		if ra.Casp1(addr, old, new) {
			return old
		}
	}
}
func SwapInt64(addr *int64, new int64) (old int64)     { return int64(ra.Xchg64(u64(addr), uint64(new))) }
func SwapUint64(addr *uint64, new uint64) (old uint64) { return ra.Xchg64(addr, new) }

func CompareAndSwapInt32(addr *int32, old, new int32) (swapped bool) {
	return ra.Cas(u32(addr), uint32(old), uint32(new))
}
func CompareAndSwapUint32(addr *uint32, old, new uint32) (swapped bool) {
	return ra.Cas(addr, old, new)
}
func CompareAndSwapUintptr(addr *uintptr, old, new uintptr) (swapped bool) {
	return ra.Cas64(up64(addr), uint64(old), uint64(new))
}
func CompareAndSwapPointer(addr *unsafe.Pointer, old, new unsafe.Pointer) (swapped bool) {
	return ra.Casp1(addr, old, new)
}
func CompareAndSwapInt64(addr *int64, old, new int64) (swapped bool) {
	return ra.Cas64(u64(addr), uint64(old), uint64(new))
}
func CompareAndSwapUint64(addr *uint64, old, new uint64) (swapped bool) {
	return ra.Cas64(addr, old, new)
}

func AddInt32(addr *int32, delta int32) (new int32)     { return int32(ra.Xadd(u32(addr), delta)) }
func AddUint32(addr *uint32, delta uint32) (new uint32) { return ra.Xadd(addr, int32(delta)) }
func AddUintptr(addr *uintptr, delta uintptr) (new uintptr) {
	return uintptr(ra.Xadd64(up64(addr), int64(delta)))
}
func AddInt64(addr *int64, delta int64) (new int64)     { return int64(ra.Xadd64(u64(addr), delta)) }
func AddUint64(addr *uint64, delta uint64) (new uint64) { return ra.Xadd64(addr, int64(delta)) }

func AndInt32(addr *int32, mask int32) (old int32)     { return int32(ra.And32(u32(addr), uint32(mask))) }
func AndUint32(addr *uint32, mask uint32) (old uint32) { return ra.And32(addr, mask) }
func AndUintptr(addr *uintptr, mask uintptr) (old uintptr) {
	return uintptr(ra.And64(up64(addr), uint64(mask)))
}
func AndInt64(addr *int64, mask int64) (old int64)     { return int64(ra.And64(u64(addr), uint64(mask))) }
func AndUint64(addr *uint64, mask uint64) (old uint64) { return ra.And64(addr, mask) }

func OrInt32(addr *int32, mask int32) (old int32)     { return int32(ra.Or32(u32(addr), uint32(mask))) }
func OrUint32(addr *uint32, mask uint32) (old uint32) { return ra.Or32(addr, mask) }
func OrUintptr(addr *uintptr, mask uintptr) (old uintptr) {
	return uintptr(ra.Or64(up64(addr), uint64(mask)))
}
func OrInt64(addr *int64, mask int64) (old int64)     { return int64(ra.Or64(u64(addr), uint64(mask))) }
func OrUint64(addr *uint64, mask uint64) (old uint64) { return ra.Or64(addr, mask) }

func LoadInt32(addr *int32) (val int32)       { return int32(ra.Load(u32(addr))) }
func LoadUint32(addr *uint32) (val uint32)    { return ra.Load(addr) }
func LoadUintptr(addr *uintptr) (val uintptr) { return uintptr(ra.Load64(up64(addr))) }
func LoadPointer(addr *unsafe.Pointer) (val unsafe.Pointer) {
	return ra.Loadp(unsafe.Pointer(addr))
}
func LoadInt64(addr *int64) (val int64)    { return int64(ra.Load64(u64(addr))) }
func LoadUint64(addr *uint64) (val uint64) { return ra.Load64(addr) }

func StoreInt32(addr *int32, val int32)       { ra.Store(u32(addr), uint32(val)) }
func StoreUint32(addr *uint32, val uint32)    { ra.Store(addr, val) }
func StoreUintptr(addr *uintptr, val uintptr) { ra.Store64(up64(addr), uint64(val)) }
func StorePointer(addr *unsafe.Pointer, val unsafe.Pointer) {
	ra.StorepNoWB(unsafe.Pointer(addr), val)
}
func StoreInt64(addr *int64, val int64)    { ra.Store64(u64(addr), uint64(val)) }
func StoreUint64(addr *uint64, val uint64) { ra.Store64(addr, val) }
