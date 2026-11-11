package main

import (
	"fmt"

	"github.com/cespare/xxhash/v2"
)

func main() {
	for _, s := range []string{"", "a", "abc", "hello world", "The quick brown fox jumps over the lazy dog", string(make([]byte, 31)), string(make([]byte, 32)), string(make([]byte, 33)), string(make([]byte, 1000))} {
		fmt.Printf("%d %016x\n", len(s), xxhash.Sum64String(s))
	}
	h := xxhash.New()
	for i := 0; i < 100; i++ {
		h.Write([]byte{byte(i), byte(i * 3), byte(i * 7), 1, 2, 3, 4})
	}
	fmt.Printf("%016x %x\n", h.Sum64(), h.Sum(nil))
	h.Reset()
	h.WriteString("abc")
	fmt.Printf("%016x %v %d\n", h.Sum64(), h.Sum64() == xxhash.Sum64([]byte("abc")), h.Size())
	d := xxhash.NewWithSeed
	_ = d
	var acc uint64
	buf := make([]byte, 4096)
	for i := range buf {
		buf[i] = byte(i * 131)
	}
	for i := 0; i < 5000; i++ {
		acc ^= xxhash.Sum64(buf[i%64 : 4000+i%64])
	}
	fmt.Printf("%016x\n", acc)
	state, _ := h.MarshalBinary()
	h2 := xxhash.New()
	fmt.Println(h2.UnmarshalBinary(state), h2.Sum64() == h.Sum64())
}
