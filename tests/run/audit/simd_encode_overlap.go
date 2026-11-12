// hex.Encode and base64 Encode/Decode with overlapping dst and src: gc's
// loops read src and write dst in order, so the result depends on the
// overlap; marmot's SIMD cores read ahead of what they write.
package main

import (
	"encoding/base64"
	"encoding/hex"
	"fmt"
)

func fill(b []byte, seed byte) {
	for i := range b {
		b[i] = seed + byte(i*7)
	}
}

func main() {
	// hex.Encode in place (dst starts at src) and dst just before src.
	for _, d := range []int{0, -4} {
		buf := make([]byte, 80)
		fill(buf, 1)
		hex.Encode(buf[20+d:20+d+32], buf[20:36])
		fmt.Printf("hex.Encode d=%d: %x\n", d, buf)
	}
	// base64 Encode in place.
	for _, enc := range []*base64.Encoding{base64.StdEncoding, base64.URLEncoding} {
		buf := make([]byte, 100)
		fill(buf, 3)
		enc.Encode(buf[10:10+enc.EncodedLen(48)], buf[10:58])
		fmt.Printf("base64.Encode in place: %x\n", buf)
	}
	// base64 Decode with dst one byte after src.
	buf := make([]byte, 40)
	copy(buf[10:], "QUJDRA==")
	n, err := base64.StdEncoding.Decode(buf[11:11+6], buf[10:18])
	fmt.Println("base64.Decode dst after src:", n, err)
	// In place (dst at src) is fine in both and stays here as a control.
	b := []byte("QUJDREVGR0hJSktMTU5PUFFSU1RVVldYWVo=")
	n, err = base64.StdEncoding.Decode(b, b)
	fmt.Printf("base64.Decode in place: %d %v %q\n", n, err, b[:n])
	h := []byte("48656c6c6f2c20776f726c64212048656c6c6f2c20776f726c6421zz")
	n, err = hex.Decode(h, h)
	fmt.Printf("hex.Decode in place: %d %v %q\n", n, err, h[:n])
}
