package main

import (
	"fmt"
	"math"
	"os"
)

type P struct {
	X, Y int
	N    string
}

type S struct{ v int }

func (s S) String() string { return fmt.Sprintf("S(%d)", s.v) }

func main() {
	// Constant format strings that a specializer might precompile.
	fmt.Printf("%d %5d %-5d| %05d %x %X %o %b %c %q %U\n", 42, 42, 42, 42, 255, 255, 8, 5, 'A', 'B', 0x1F600)
	fmt.Printf("%s|%10s|%-10s|%.2s|%q|%v|%x\n", "go", "go", "go", "golang", "q\"", "v", "hex")
	fmt.Printf("%f|%.0f|%8.3f|%-8.2f|%e|%g|%G|%+.1f\n", 3.14159, 2.5, 3.14159, 3.14159, 123456.789, 1e21, 1e-7, 2.0)
	fmt.Printf("%v %+v %#v %T\n", P{1, 2, "n"}, P{1, 2, "n"}, P{1, 2, "n"}, P{})
	fmt.Printf("%v %v %v\n", []int{1, 2}, map[string]int{"b": 1, "a": 2}, [2]bool{true, false})
	fmt.Printf("%v %s %d\n", S{3}, S{4}, S{5})
	fmt.Printf("%t %v\n", true, nil)
	fmt.Printf("%6.2f%%|%+d|% d|%x\n", 99.5, 7, 7, -255)
	fmt.Printf("%*d|%-*d|%.*f\n", 5, 1, 5, 2, 2, 3.14159)
	fmt.Printf("%[2]d %[1]d %[2]d\n", 1, 2)
	fmt.Println(math.Inf(1), math.Inf(-1), math.NaN(), -0.0, float32(0.1), 1e21, 1e20, 123456789.0)
	fmt.Println(int8(-5), uint8(250), 'x', "s", nil, []string{}, map[int]int{}, struct{}{})
	var ip *int
	var e error
	fmt.Println(ip, e, []byte("hi"), [3]byte{1, 2, 3})
	fmt.Fprintln(os.Stdout, "a", 1, 2, "b", "c", 3.0)
	fmt.Print("a", "b", 1, 2, "c", 3.5, "\n")
	fmt.Print(1, 2, "\n")
	fmt.Println(fmt.Sprint("a", 1, 2, "b"), fmt.Sprint(1, 2), fmt.Sprintf("%05.1f|%+05d|%x", -2.5, 42, "é"))
	for _, f := range []float64{0, 1, 0.1, 100, 1e6, 1e-5, 123456789, 0.000123, math.MaxFloat64, math.SmallestNonzeroFloat64} {
		fmt.Printf("%v|%g|%e|%.3g|%8.2f\n", f, f, f, f, f)
	}
}
