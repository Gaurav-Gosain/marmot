package main

import "fmt"

type P struct {
	X, Y int
	N    string
}

type S struct{}

func (S) String() string { return "S!" }

func main() {
	fmt.Println("hello", 42, 3.5, true, nil, 'x')
	fmt.Printf("%d %5d %-5d| %05d %x %X %o %b %c %U\n", 42, 42, 42, 42, 255, 255, 8, 5, 'A', 0x1F600)
	fmt.Printf("%s %q %10s|%-10s| %v\n", "str", "q", "r", "l", "v")
	fmt.Printf("%f %.2f %8.3f %e %g %G\n", 3.14159, 3.14159, 3.14159, 1234.5678, 0.000001234, 1e21)
	fmt.Printf("%t %v\n", true, false)
	fmt.Printf("%v %+v %#v\n", P{1, 2, "n"}, P{1, 2, "n"}, P{1, 2, "n"})
	fmt.Printf("%v %v %v\n", []int{1, 2}, map[string]int{"a": 1, "b": 2}, [2]bool{true})
	fmt.Printf("%T %T %T %T\n", 1, "s", P{}, []string{})
	fmt.Printf("%v %s\n", S{}, S{})
	fmt.Printf("%v %d\n", &P{1, 2, "p"}, []int{3, 4})
	fmt.Printf("%x %X % x\n", "hi", []byte("hi"), "abc")
	fmt.Printf("%%|%6.2f|%+d|%v\n", 2.5, 5, struct{}{})
	bad := []string{"%d %s\n", "%d\n", "%d %d\n"}
	fmt.Printf(bad[0], "str", 5)
	fmt.Printf(bad[1], []any{}...)
	fmt.Printf(bad[2], 1, 2, 3)
}
