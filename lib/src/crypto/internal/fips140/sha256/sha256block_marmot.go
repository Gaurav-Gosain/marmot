// marmot overlay: the SHA-256 block function runs on the C core's
// rt_sha256_block (ARMv8 SHA2 instructions on arm64, SHA-NI on x86-64,
// portable C otherwise) instead of assembly or the pure Go version.

package sha256

import "unsafe"

//go:linkname rt_sha256_block rt_sha256_block
func rt_sha256_block(h *uint32, p unsafe.Pointer, n int)

func block(dig *Digest, p []byte) {
	if len(p) >= chunk {
		rt_sha256_block(&dig.h[0], unsafe.Pointer(unsafe.SliceData(p)), len(p))
	}
}
