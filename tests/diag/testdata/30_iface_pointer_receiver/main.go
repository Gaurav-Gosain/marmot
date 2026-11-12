package main

type Counter interface{ Inc() }

type C struct{ n int }

func (c *C) Inc() { c.n++ }

func use(x Counter) { x.Inc() }

func main() {
	var c C
	use(c)
}
