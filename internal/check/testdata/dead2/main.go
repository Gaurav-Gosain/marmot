package main

import "fmt"

// An unexported interface method reaches the types in the table.
type format interface{ apply() int }

type flat struct{}

func (flat) apply() int { return 1 }

type nested struct{}

func (nested) apply() int { return 2 }

func (nested) unusedHelper() int { return 3 } // dead: never called

var table = []format{flat{}, nested{}}

// Exported methods of a type converted to an interface are reachable
// through reflection.
type named struct{}

func (named) String() string { return "n" }

func (named) Extra() {}

type hidden struct{}

func (hidden) String() string { return "h" } // dead: never converted

// A method call on a type parameter reaches the methods of that name of
// the type arguments.
type getter interface{ get() int }

type box struct{ v int }

func (b box) get() int { return b.v }

func use[T getter](x T) int { return x.get() }

func callback() {} // reachable: its address is taken

func dead() {} // dead

func main() {
	for _, f := range table {
		f.apply()
	}
	fmt.Println(named{})
	use(box{1})
	f := callback
	f()
	var h hidden
	_ = h
}
