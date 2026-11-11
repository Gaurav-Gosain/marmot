package main

import (
	"crypto/md5"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"fmt"
	"hash/crc32"
	"hash/fnv"
	"strings"
)

func main() {
	for _, s := range []string{"", "abc", "The quick brown fox jumps over the lazy dog", strings.Repeat("a", 1000)} {
		fmt.Printf("%x\n%x\n%x\n", sha256.Sum256([]byte(s)), sha1.Sum([]byte(s)), md5.Sum([]byte(s)))
		fmt.Printf("%x\n", sha512.Sum512([]byte(s)))
		fmt.Println(crc32.ChecksumIEEE([]byte(s)))
	}
	h := sha256.New()
	for i := 0; i < 100; i++ {
		fmt.Fprintf(h, "chunk %d;", i)
	}
	fmt.Printf("%x %d %d\n", h.Sum(nil), h.Size(), h.BlockSize())
	f := fnv.New32a()
	f.Write([]byte("hello"))
	f64 := fnv.New64a()
	f64.Write([]byte("hello"))
	fmt.Println(f.Sum32(), f64.Sum64())
}
