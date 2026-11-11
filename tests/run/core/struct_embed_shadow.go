package main

type A struct{ V int }

func (A) Who() string { return "A" }

type B struct{ V int }

func (B) Who() string { return "B" }

type C struct {
	A
	B
	V string
}

type D struct {
	A
}

func (D) Who() string { return "D" }

func main() {
	c := C{A{1}, B{2}, "c"}
	println(c.V, c.A.V, c.B.V)
	println(c.A.Who(), c.B.Who())
	d := D{A{3}}
	println(d.Who(), d.A.Who(), d.V)
}
