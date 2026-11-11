package main

import (
	"bytes"
	"fmt"
	"math/rand"
	"time"

	"github.com/oklog/ulid/v2"
)

func main() {
	entropy := rand.New(rand.NewSource(1))
	ts := time.Unix(1700000000, 123000000)
	id, err := ulid.New(ulid.Timestamp(ts), entropy)
	fmt.Println(id, err, id.Time(), ulid.Time(id.Time()).UTC())
	id2 := ulid.MustNew(ulid.Timestamp(ts), entropy)
	fmt.Println(id2, id.Compare(id2), id2.Compare(id), id.Compare(id))
	p, err := ulid.Parse(id.String())
	fmt.Println(p == id, err)
	for _, s := range []string{"", "01ARZ3NDEKTSV4RRFFQ69G5FAV", "01ARZ3NDEKTSV4RRFFQ69G5FA", "81ARZ3NDEKTSV4RRFFQ69G5FAV", "01ARZ3NDEKTSV4RRFFQ69G5FAU", "zzzzzzzzzzzzzzzzzzzzzzzzzz"} {
		u, err := ulid.Parse(s)
		fmt.Println(u, err)
	}
	var z ulid.ULID
	fmt.Println(z, z == ulid.ULID{}, id.Entropy()[:3], len(id.Bytes()))
	b, _ := id.MarshalBinary()
	var back ulid.ULID
	fmt.Println(back.UnmarshalBinary(b), back == id, bytes.Equal(b, id.Bytes()))
	m := ulid.Monotonic(rand.New(rand.NewSource(2)), 0)
	prev := ulid.MustNew(1000, m)
	ok := true
	for i := 0; i < 1000; i++ {
		n := ulid.MustNew(1000, m)
		if n.Compare(prev) <= 0 {
			ok = false
		}
		prev = n
	}
	fmt.Println(ok, prev)
	var tt ulid.ULID
	fmt.Println(tt.SetTime(ulid.MaxTime()), tt.SetTime(ulid.MaxTime()+1), ulid.MaxTime())
}
