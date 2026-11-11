package main

type Counter struct{ n int }

func (c *Counter) Inc()     { c.n++ }
func (c Counter) Get() int  { return c.n }
func (c Counter) Plus(k int) Counter { c.n += k; return c }

func main() {
	var c Counter
	c.Inc()
	c.Inc()
	println(c.Get())
	d := c.Plus(10)
	println(c.n, d.n)
	p := &c
	p.Inc()
	println(p.Get(), (*p).Get())
	cs := []Counter{{}, {}}
	cs[0].Inc()
	println(cs[0].n, cs[1].n)
	m := map[string]*Counter{"a": {}}
	m["a"].Inc()
	println(m["a"].n)
	arr := [2]Counter{}
	arr[1].Inc()
	println(arr[1].n)
}
