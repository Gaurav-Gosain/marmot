// Invokes on interfaces with one dynamic type (MARMOT_DEVIRT1): escape
// analysis uses the method the call reaches. A method that keeps its
// argument must keep it alive; a method that only reads it may let the
// caller's buffer stay in the frame.
package main

import (
	"fmt"
	"hash"
	"hash/fnv"
	"strings"
)

type Writer interface{ Write([]byte) (int, error) }

type keeper struct{ last [][]byte }

func (k *keeper) Write(b []byte) (int, error) { k.last = append(k.last, b); return len(b), nil }

type counter struct{ n int }

func (c *counter) Write(b []byte) (int, error) {
	for _, x := range b {
		c.n += int(x)
	}
	return len(b), nil
}

type valueW struct{ tag string }

func (v valueW) Write(b []byte) (int, error) { return len(b) + len(v.tag), nil }

func newKeeper() Writer           { return &keeper{} }
func newCounter() Writer          { return &counter{} }
func newValue() Writer            { return valueW{"v"} }
func newMaybe(c bool) Writer      { if c { return nil }; return &counter{} }

// keepLocal: the keeper keeps both buffers, which must outlive the frame.
//
//go:noinline
func keepLocal(i int) [][]byte {
	k := newKeeper()
	buf := []byte{byte('a' + i), byte('b' + i), byte('c' + i)}
	k.Write(buf)
	s := fmt.Sprint(i)
	k.Write([]byte(s))
	return k.(*keeper).last
}

// countLocal: the counter only reads.
//
//go:noinline
func countLocal(i int) int {
	c := newCounter()
	buf := []byte{byte('a' + i), byte('b' + i)}
	c.Write(buf)
	c.Write([]byte(fmt.Sprint(i)))
	return c.(*counter).n
}

//go:noinline
func scribble() int {
	var junk [64]byte
	for i := range junk {
		junk[i] = 'Z'
	}
	return int(junk[3])
}

func main() {
	var kept [][]byte
	for i := 0; i < 3; i++ {
		kept = append(kept, keepLocal(i)...)
		scribble()
	}
	var parts []string
	for _, b := range kept {
		parts = append(parts, string(b))
	}
	fmt.Println(strings.Join(parts, ","))

	total := 0
	for i := 0; i < 3; i++ {
		total += countLocal(i)
	}
	fmt.Println(total)

	v := newValue()
	n, _ := v.Write([]byte("xyz"))
	fmt.Println(n)

	var h hash.Hash64 = fnv.New64a()
	for i := 0; i < 1000; i++ {
		msg := fmt.Sprintf("line %d", i)
		h.Write([]byte(msg))
		h.Write([]byte{'\n'})
	}
	fmt.Println(h.Sum64())

	m := newMaybe(true)
	func() {
		defer func() { fmt.Println("nil interface:", recover() != nil) }()
		m.Write([]byte("x"))
	}()
}
