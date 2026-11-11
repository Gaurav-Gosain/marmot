package main

type P struct {
	X, Y int
	Name string
}

type Mixed struct {
	A bool
	B int64
	C uint8
	D float32
	E string
	F []int
}

func main() {
	p := P{1, 2, "pt"}
	q := p
	q.X = 10
	println(p.X, q.X, p.Name, q.Name)
	pp := &p
	pp.Y = 20
	println(p.Y)
	var z P
	println(z.X, z.Y, z.Name == "")
	m := Mixed{A: true, B: -5, C: 200, D: 1.5, E: "e"}
	println(m.A, m.B, m.C, m.D, m.E, m.F == nil)
	anon := struct {
		a int
		b string
	}{3, "x"}
	println(anon.a, anon.b)
	n := P{Name: "only"}
	println(n.X, n.Name)
}
