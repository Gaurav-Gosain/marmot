// appendcap: properties of append growth that do not depend on the implementation's choice
// of capacity: cap >= len after every append, the elements are kept, and when append
// reallocates, the old backing array is unchanged. (verify/lean/SliceGrow.lean proves
// cap >= len at every exit of nextslicecap.) Capacities themselves are not printed: stock
// Go 1.25 starts small non-escaping slices on the stack with other capacities.
package main

import "fmt"

type e3 [3]byte
type e24 struct{ a, b, c int64 }

func main() {
	var a []byte
	var b []int64
	var c []e3
	var d []e24
	ok := true
	grows := 0
	for i := 0; i < 20000; i++ {
		old := b
		a = append(a, byte(i))
		b = append(b, int64(i))
		c = append(c, e3{byte(i)})
		d = append(d, e24{a: int64(i)})
		if cap(a) < len(a) || cap(b) < len(b) || cap(c) < len(c) || cap(d) < len(d) {
			ok = false
		}
		if len(old) > 0 && cap(old) != cap(b) {
			grows++
			if &old[0] == &b[0] || old[len(old)-1] != int64(i-1) {
				ok = false
			}
		}
	}
	g := append([]int64{1}, make([]int64, 100000)...)
	ok = ok && len(g) == 100001 && cap(g) >= len(g)
	s := 0
	for i := range b {
		s += int(b[i]) - i + int(d[i].a) - i + int(c[i][0]) - i%256 + int(a[i]) - i%256
	}
	fmt.Println("ok", ok, "kept", s == 0, "grew", grows > 10 && grows < 60)
}
