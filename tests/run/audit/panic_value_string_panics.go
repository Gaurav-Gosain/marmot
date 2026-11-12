// Audit (spec, 2026-11-12): the value of an unrecovered panic is printed
// with its String method; when that method panics too, gc stops with
// "fatal error: panic while printing panic value". marmot's runtime calls
// gopanic -> preprintpanics -> String -> gopanic ... without end (the
// process spins at full CPU).
package main

type P int

func (p P) String() string { panic("again") }

func main() {
	panic(P(1))
}
