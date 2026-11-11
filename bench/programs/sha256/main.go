// sha256: hash a large buffer repeatedly.
package main

import (
	"crypto/sha256"
	"fmt"
)

func main() {
	buf := make([]byte, 1<<20)
	for i := range buf {
		buf[i] = byte(i * 7)
	}
	var sum [32]byte
	for r := 0; r < 24000; r++ {
		buf[0] = byte(r)
		sum = sha256.Sum256(buf)
		buf[1] = sum[0]
	}
	fmt.Printf("%x\n", sum)
}
