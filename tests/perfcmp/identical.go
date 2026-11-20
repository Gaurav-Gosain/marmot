package main

import (
	"bytes"
	"io"
	"os"
)

// sameBinary reports whether binaries a and b, built from the marmot trees rootA
// and rootB, are the same: byte for byte, or once every rootB in b reads rootA.
// Builds from two trees embed their root path (source and library paths), so
// two trees at the same code differ in those bytes only. The paths must have
// the same length: a replacement then moves no other byte, and the layout of
// the files stays comparable. Other lengths compare byte for byte.
func sameBinary(a, b, rootA, rootB string) bool {
	if sameFile(a, b) {
		return true
	}
	if rootA == "" || rootA == rootB || len(rootA) != len(rootB) {
		return false
	}
	da, err := os.ReadFile(a)
	if err != nil {
		return false
	}
	db, err := os.ReadFile(b)
	if err != nil || len(da) != len(db) {
		return false
	}
	return bytes.Equal(da, bytes.ReplaceAll(db, []byte(rootB), []byte(rootA)))
}

// sameFile reports whether the files a and b have the same bytes. An error
// reading either counts as different, so the program is timed as usual.
func sameFile(a, b string) bool {
	fa, err := os.Open(a)
	if err != nil {
		return false
	}
	defer fa.Close()
	fb, err := os.Open(b)
	if err != nil {
		return false
	}
	defer fb.Close()
	sa, err1 := fa.Stat()
	sb, err2 := fb.Stat()
	if err1 != nil || err2 != nil || sa.Size() != sb.Size() {
		return false
	}
	ba, bb := make([]byte, 1<<20), make([]byte, 1<<20)
	for {
		na, ea := io.ReadFull(fa, ba)
		nb, eb := io.ReadFull(fb, bb)
		if na != nb || !bytes.Equal(ba[:na], bb[:nb]) {
			return false
		}
		if ea == io.EOF || ea == io.ErrUnexpectedEOF {
			return eb == ea
		}
		if ea != nil || eb != nil {
			return false
		}
	}
}
