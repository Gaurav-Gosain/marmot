// Audit probe (loops, 2026-11-12): held on HEAD 86cadb6 in dev, -release and
// GC stress. Kept as a guard; see docs/audit-backend.md.
package main

import "fmt"

var global string

//go:noinline
func empty() string { return "" }

// string(b) + e with e == "" at run time: runtime.concatstring2 returns
// string(b) itself. If string(b) was not copied, the result shares b.
//
//go:noinline
func join(b []byte, e string) string {
	return string(b) + e
}

//go:noinline
func stackBuf() {
	b := make([]byte, 4)
	b[0], b[1], b[2], b[3] = 'w', 'x', 'y', 'z'
	global = string(b) + empty()
}

//go:noinline
func clobber() int {
	var buf [256]byte
	for i := range buf {
		buf[i] = '#'
	}
	n := 0
	for _, c := range buf {
		n += int(c)
	}
	return n
}

func main() {
	b := []byte("abcd")
	s := join(b, empty())
	b[0] = 'Z'
	fmt.Println(s)

	m := map[string]int{}
	k := []byte("key1")
	m[string(k)+empty()] = 1
	k[3] = '2'
	m[string(k)+empty()] = 2
	fmt.Println(len(m), m["key1"], m["key2"])

	stackBuf()
	clobber()
	fmt.Println(global)
}
