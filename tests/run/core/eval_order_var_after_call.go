package main

// The Go spec leaves the order of a plain variable read versus a function call in the same
// expression unspecified. gc reads the variable after the calls. Real code depends on it, for
// example `return id, parse(s, &id)` in github.com/oklog/ulid.

type ID [4]int

func set(p *ID, v int) error {
	for i := range p {
		p[i] = v
	}
	return nil
}

func parse(v int) (id ID, err error) {
	return id, set(&id, v)
}

func parsePtr(v int) (n int, err error) {
	return n, bump(&n, v)
}

func bump(p *int, v int) error {
	*p += v
	return nil
}

var global = 1

func touch() int {
	global = 100
	return 5
}

func main() {
	id, err := parse(7)
	println(id[0], id[3], err == nil)
	n, _ := parsePtr(9)
	println(n)
	a := global + touch()
	println(a)
	global = 1
	pair := [2]int{global, touch()}
	println(pair[0], pair[1])
	global = 1
	println(global, touch(), global)
	x := 1
	f := func() int { x = 50; return 1 }
	y := x + f()
	println(y)
	s := []int{1, 2, 3}
	g := func() int { s[0] = 40; return 0 }
	println(s[0] + g())
}
