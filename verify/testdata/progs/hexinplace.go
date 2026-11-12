// hex.Decode with dst and src sharing memory, on input with a bad byte
// after some valid pairs. Stock Go decodes the valid pairs and reports the
// real bad byte. marmot's overlay lets the C decoder write dst (= src)
// before it finds the bad byte, then reruns the Go loop from the start
// over the overwritten src.
package main

import (
	"encoding/hex"
	"fmt"
)

func main() {
	b := []byte("0000zz")
	n, err := hex.Decode(b, b)
	fmt.Println(n, err)

	// Same with more than one 16-byte NEON block decoded first.
	c := []byte("0000000000000000000000000000000000000000zz")
	n, err = hex.Decode(c, c)
	fmt.Println(n, err)
}
