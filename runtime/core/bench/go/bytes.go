// Go counterpart of ../bytes.c. Run: go run bytes.go
package main

import (
	"bytes"
	"fmt"
	"hash/maphash"
	"time"
	"unicode/utf8"
)

var sink int

func bench(name string, nbytes int, reps int, f func() int) {
	t0 := time.Now()
	for r := 0; r < reps; r++ {
		sink += f()
	}
	s := time.Since(t0).Seconds()
	fmt.Printf("%-34s %8.2f GB/s  %8.1f ns/op\n", name, float64(nbytes)*float64(reps)/s/1e9, s*1e9/float64(reps))
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

func main() {
	n := 1 << 20
	buf := make([]byte, n)
	for i := range buf {
		buf[i] = byte('a' + i%23)
	}
	ub := make([]byte, n)
	for i := 0; i+2 <= n; i += 2 {
		if i%14 != 0 {
			copy(ub[i:], "ab")
		} else {
			copy(ub[i:], "\xc3\xa9")
		}
	}
	seed := maphash.MakeSeed()
	cpy := append([]byte(nil), buf...)
	bench("Count 1MiB", n, 2000, func() int { return bytes.Count(buf, []byte{'q'}) })
	bench("IndexByte 1MiB (absent)", n, 2000, func() int { return bytes.IndexByte(buf, 'Z') })
	bench("Index 1MiB 8-byte sep (absent)", n, 2000, func() int { return bytes.Index(buf, []byte("abcdefgZ")) })
	bench("utf8.Valid 1MiB ASCII", n, 2000, func() int { return b2i(utf8.Valid(buf)) })
	bench("utf8.Valid 1MiB mixed", n, 500, func() int { return b2i(utf8.Valid(ub)) })
	bench("RuneCount 1MiB ASCII", n, 2000, func() int { return utf8.RuneCount(buf) })
	bench("hash 1MiB", n, 2000, func() int { return int(maphash.Bytes(seed, buf)) })
	bench("hash 16B", 16, 50000000, func() int { return int(maphash.Bytes(seed, buf[sink&7:sink&7+16])) })
	bench("Equal 1MiB", n, 2000, func() int { return b2i(bytes.Equal(buf, cpy)) })
	bench("Index 64B in short string", 64, 20000000, func() int { return bytes.Index(buf[sink&1:sink&1+64], []byte("uvw")) })
}
