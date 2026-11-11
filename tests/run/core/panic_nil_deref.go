package main

type T struct{ a, b int }

func main() {
	var p *T
	println("start")
	println(p.b)
}
