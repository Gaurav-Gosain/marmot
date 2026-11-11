package main

import (
	"errors"
	"fmt"
)

type S struct{ n int }

func (s S) String() string { return fmt.Sprintf("S<%d>", s.n) }

type PS struct{ n int }

func (p *PS) String() string { return "PS!" }

type G struct{ v int }

func (g G) GoString() string { return "G!" }

type F struct{}

func (F) Format(f fmt.State, verb rune) { fmt.Fprintf(f, "F(%c)", verb) }

type Err struct{}

func (Err) Error() string { return "an error" }

type Both struct{}

func (Both) Error() string  { return "as error" }
func (Both) String() string { return "as string" }

func main() {
	fmt.Println(S{1}, &S{2}, []S{{3}, {4}}, map[string]S{"k": {5}})
	fmt.Printf("%v %s %d %+v\n", S{1}, S{1}, S{1}, S{1})
	fmt.Println(PS{1}, &PS{1})
	fmt.Printf("%v %#v\n", G{1}, G{1})
	fmt.Printf("%v %d %s\n", F{}, F{}, F{})
	fmt.Println(Err{}, Both{}, errors.New("plain"))
	fmt.Printf("%v %s %q\n", Err{}, Err{}, Err{})
	var np *S
	fmt.Printf("%v\n", np == nil)
	fmt.Println(fmt.Sprint(S{9}), fmt.Sprintf("%6v|%-6v|", S{1}, S{2}))
}
