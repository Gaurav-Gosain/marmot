package main

import (
	"encoding/ascii85"
	"encoding/asn1"
	"encoding/base32"
	"encoding/pem"
	"fmt"
	"math/big"
)

type Rec struct {
	A int
	B string
	C []int
	D bool
	E big.Int
	F asn1.ObjectIdentifier
}

func main() {
	r := Rec{A: 42, B: "hi", C: []int{1, 2, 3}, D: true, F: asn1.ObjectIdentifier{1, 2, 840, 113549}}
	r.E.SetString("123456789012345678901234567890", 10)
	der, err := asn1.Marshal(r)
	fmt.Printf("%x %v\n", der, err)
	var back Rec
	rest, err := asn1.Unmarshal(der, &back)
	fmt.Println(len(rest), err, back.A, back.B, back.C, back.D, back.E.String(), back.F)
	_, err = asn1.Unmarshal(der[:5], &back)
	fmt.Println(err != nil)
	blk := &pem.Block{Type: "TEST DATA", Headers: map[string]string{"K": "v"}, Bytes: []byte("0123456789abcdef0123456789abcdef0123456789abcdef")}
	enc := pem.EncodeToMemory(blk)
	fmt.Print(string(enc))
	d, rest2 := pem.Decode(append(enc, "trailing"...))
	fmt.Println(d.Type, d.Headers, string(d.Bytes) == string(blk.Bytes), string(rest2))
	d, _ = pem.Decode([]byte("not pem"))
	fmt.Println(d == nil)
	for _, s := range []string{"", "f", "fo", "foo", "foob", "fooba", "foobar"} {
		e := base32.StdEncoding.EncodeToString([]byte(s))
		h := base32.HexEncoding.WithPadding(base32.NoPadding).EncodeToString([]byte(s))
		dec, err := base32.StdEncoding.DecodeString(e)
		fmt.Println(e, h, string(dec), err)
	}
	dst := make([]byte, ascii85.MaxEncodedLen(11))
	n := ascii85.Encode(dst, []byte("hello world"))
	back2 := make([]byte, 11)
	nd, _, err := ascii85.Decode(back2, dst[:n], true)
	fmt.Println(string(dst[:n]), string(back2[:nd]), err)
	_, err = base32.StdEncoding.DecodeString("1!")
	fmt.Println(err)
}
