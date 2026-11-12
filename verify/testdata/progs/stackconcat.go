// stackconcat: string(b) in a frame buffer (MARMOT_STACKCONV) used as an operand of string
// concatenation. The runtime returns an operand itself when the others are empty (as gc's
// does), so the concatenation's result aliases the conversion: if the result escapes, the
// conversion escapes. The prefixes are empty at run time but not constant.
package main

import (
	"fmt"
	"os"
	"strings"
)

var empty = ""

func init() {
	if len(os.Args) > 5 {
		empty = "x"
	}
}

//go:noinline
func two(b []byte) string { return empty + string(b) }

//go:noinline
func three(b []byte) string { return empty + string(b) + empty }

//go:noinline
func many(b []byte) string { return empty + empty + string(b) + empty + empty }

//go:noinline
func acc(bs [][]byte) string {
	s := empty
	for _, b := range bs {
		s += string(b)
	}
	return s
}

var sink string

//go:noinline
func global(b []byte) { sink = empty + string(b) }

//go:noinline
func viaMap(b []byte, m map[int]string) { m[1] = string(b) + empty }

//go:noinline
func smash() {
	var big [256]byte
	for i := range big {
		big[i] = '#'
	}
	_ = strings.Count(string(big[:]), "#")
}

func main() {
	b := []byte("abc")
	r := []string{two(b), three(b), many(b), acc([][]byte{b})}
	global(b)
	m := map[int]string{}
	viaMap(b, m)
	r = append(r, sink, m[1])
	smash()
	b[0] = 'z'
	smash()
	fmt.Printf("%q\n", r)
}
