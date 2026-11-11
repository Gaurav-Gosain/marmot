// marmot overlay: the architecture hooks of hash/crc32 run on the C core's
// rt_crc32_update (ARMv8 CRC32 instructions on arm64, a byte table
// elsewhere) for the IEEE and Castagnoli polynomials; Koopman and other
// tables keep the portable code.

package crc32

import "unsafe"

//go:linkname rt_crc32_update rt_crc32_update
func rt_crc32_update(crc uint32, p unsafe.Pointer, n int, kind int32) uint32

func archAvailableIEEE() bool { return true }
func archInitIEEE()           {}

func archUpdateIEEE(crc uint32, p []byte) uint32 {
	return rt_crc32_update(crc, unsafe.Pointer(unsafe.SliceData(p)), len(p), 0)
}

func archAvailableCastagnoli() bool { return true }
func archInitCastagnoli()           {}

func archUpdateCastagnoli(crc uint32, p []byte) uint32 {
	return rt_crc32_update(crc, unsafe.Pointer(unsafe.SliceData(p)), len(p), 1)
}
