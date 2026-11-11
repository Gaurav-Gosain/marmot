package main

type Big struct{ a, b, c, d, e int }

type I interface{ Get() int }

type Small int8
type Ptr struct{ n int }
type Arr [3]int

func (s Small) Get() int { return int(s) }
func (p *Ptr) Get() int  { return p.n }
func (b Big) Get() int   { return b.a + b.e }
func (a Arr) Get() int   { return a[0] + a[2] }

func main() {
	var xs []I
	xs = append(xs, Small(-3), &Ptr{4}, Big{1, 2, 3, 4, 5}, Arr{7, 8, 9})
	for _, x := range xs {
		println(x.Get())
	}
	b := Big{1, 0, 0, 0, 1}
	var i I = b
	b.a = 100
	println(i.Get(), b.Get())
	p := &Ptr{1}
	var j I = p
	p.n = 50
	println(j.Get())
	var e interface{} = 42
	var f interface{} = "str"
	var g interface{} = 3.5
	var h interface{} = true
	println(e.(int), f.(string), g.(float64), h.(bool))
	var k interface{} = int8(5)
	_, ok := k.(int)
	println(ok)
	n, ok := k.(int8)
	println(n, ok)
}
