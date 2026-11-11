// marmot overlay: the SHA-512 block function runs on the C core's
// rt_sha512_block (ARMv8.2 SHA512 instructions on arm64, portable C
// otherwise) instead of assembly or the pure Go version.

package sha512

import "unsafe"

//go:linkname rt_sha512_block rt_sha512_block
//go:noescape
func rt_sha512_block(h *uint64, p unsafe.Pointer, n int)

func block(dig *Digest, p []byte) {
	if len(p) >= chunk {
		rt_sha512_block(&dig.h[0], unsafe.Pointer(unsafe.SliceData(p)), len(p))
	}
}
