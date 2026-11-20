package main

import (
	"bytes"
	"io"
	"os"
)

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
