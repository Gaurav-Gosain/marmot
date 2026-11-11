package main

import (
	"fmt"

	"github.com/mr-tron/base58"
)

func main() {
	for _, s := range []string{"", "a", "abc", "Hello World!", "\x00\x00hello", "\x00", "\xff\xff\xff\xff"} {
		e := base58.Encode([]byte(s))
		d, err := base58.Decode(e)
		fmt.Printf("%q %q %v %v\n", s, e, string(d) == s, err)
	}
	for _, bad := range []string{"0OIl", "abc!", "1111", " "} {
		d, err := base58.Decode(bad)
		fmt.Printf("%q %v %v\n", bad, d, err)
	}
	fmt.Println(base58.FastBase58Encoding([]byte("fast")), base58.EncodeAlphabet([]byte("x"), base58.FlickrAlphabet))
	big := make([]byte, 1000)
	for i := range big {
		big[i] = byte(i * 7)
	}
	e := base58.Encode(big)
	d, _ := base58.Decode(e)
	fmt.Println(len(e), string(d) == string(big))
}
