package main

type Container[T any] interface {
	Put(T)
	Get() T
}

type Box[T any] struct{ v T }

func (b *Box[T]) Put(v T) { b.v = v }
func (b *Box[T]) Get() T  { return b.v }

func Use[T any, C Container[T]](c C, v T) T {
	c.Put(v)
	return c.Get()
}

func Fill[T any](c Container[T], v T) {
	c.Put(v)
}

type Opt[T any] struct {
	val T
	ok  bool
}

func Some[T any](v T) Opt[T] { return Opt[T]{v, true} }
func None[T any]() Opt[T]    { return Opt[T]{} }
func (o Opt[T]) Or(d T) T {
	if o.ok {
		return o.val
	}
	return d
}

type Tree[T any] struct {
	L, R *Tree[T]
	V    T
}

func (t *Tree[T]) Walk(f func(T)) {
	if t == nil {
		return
	}
	t.L.Walk(f)
	f(t.V)
	t.R.Walk(f)
}

func main() {
	b := &Box[int]{}
	println(Use[int](b, 5))
	var c Container[string] = &Box[string]{}
	Fill(c, "hi")
	println(c.Get())
	println(Some(3).Or(0), None[int]().Or(9), None[string]().Or("d"))
	t := &Tree[int]{&Tree[int]{nil, nil, 1}, &Tree[int]{nil, nil, 3}, 2}
	t.Walk(func(i int) { println(i) })
}
