package main

import (
	"fmt"

	"github.com/google/uuid"
)

func main() {
	ns := uuid.NameSpaceDNS
	a := uuid.NewMD5(ns, []byte("example.com"))
	b := uuid.NewSHA1(ns, []byte("example.com"))
	fmt.Println(a, b, a.Version(), b.Version(), a.Variant())
	p, err := uuid.Parse("6ba7b810-9dad-11d1-80b4-00c04fd430c8")
	fmt.Println(p == ns, err, p.URN(), p.ID())
	for _, s := range []string{"", "zzz", "6ba7b8109dad11d180b400c04fd430c8", "{6ba7b810-9dad-11d1-80b4-00c04fd430c8}", "urn:uuid:6ba7b810-9dad-11d1-80b4-00c04fd430c8", "6ba7b810-9dad-11d1-80b4-00c04fd430cg"} {
		u, err := uuid.Parse(s)
		fmt.Println(u, err)
	}
	var zero uuid.UUID
	fmt.Println(zero, zero == uuid.Nil, uuid.Max)
	bs, _ := a.MarshalText()
	var back uuid.UUID
	fmt.Println(string(bs), back.UnmarshalText(bs), back == a)
	u7 := uuid.Must(uuid.NewV7())
	fmt.Println(u7.Version(), len(u7.String()))
	n1, n2 := uuid.New(), uuid.New()
	fmt.Println(n1 != n2, n1.Version())
	v, _ := a.Value()
	fmt.Println(v)
	var sc uuid.UUID
	fmt.Println(sc.Scan("6ba7b810-9dad-11d1-80b4-00c04fd430c8"), sc == ns)
	fmt.Println(uuid.MustParse("00000000-0000-0000-0000-000000000001").String())
}
