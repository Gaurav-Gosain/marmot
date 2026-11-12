package main

import "strings"

type Counter struct {
	n     int
	label string
}

func (c *Counter) Increment() { c.n++ }

func (c Counter) Value() int { return c.n }

func main() {
	var c Counter
	c.Incremnt()
	c.Lable = "x"
	var b strings.Builder
	b.WriteStrin("x")
}
