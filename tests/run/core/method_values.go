package main

type T struct{ n int }

func (t T) Val() int   { return t.n }
func (t *T) Ptr() int  { return t.n * 10 }
func (t *T) Set(n int) { t.n = n }

func main() {
	t := T{1}
	v := t.Val
	p := t.Ptr
	t.n = 2
	println(v(), p())
	s := t.Set
	s(7)
	println(t.n)
	f := T.Val
	println(f(T{5}))
	g := (*T).Ptr
	println(g(&T{6}))
	h := (*T).Val
	println(h(&T{8}))
	k := (*T).Set
	k(&t, 33)
	println(t.n)
	fs := []func() int{t.Val, t.Ptr}
	t.n = 4
	println(fs[0](), fs[1]())
}
