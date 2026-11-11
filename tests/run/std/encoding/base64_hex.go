package main

import (
	"encoding/base64"
	"encoding/hex"
	"fmt"
)

func main() {
	for _, s := range []string{"", "f", "fo", "foo", "foob", "fooba", "foobar", "\xff\xfe\xfd", "héllo wörld"} {
		e := base64.StdEncoding.EncodeToString([]byte(s))
		u := base64.URLEncoding.EncodeToString([]byte(s))
		r := base64.RawStdEncoding.EncodeToString([]byte(s))
		d, err := base64.StdEncoding.DecodeString(e)
		fmt.Println(e, u, r, string(d) == s, err, hex.EncodeToString([]byte(s)))
	}
	_, err := base64.StdEncoding.DecodeString("!!!")
	fmt.Println(err)
	_, err = base64.StdEncoding.DecodeString("Zm9")
	fmt.Println(err)
	b, err := hex.DecodeString("48656c6c6f")
	fmt.Println(string(b), err)
	_, err = hex.DecodeString("4g")
	fmt.Println(err)
	_, err = hex.DecodeString("123")
	fmt.Println(err)
	fmt.Print(hex.Dump([]byte("0123456789abcdefXYZ")))
}
