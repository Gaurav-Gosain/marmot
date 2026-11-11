package main

type Tree struct{ l, r *Tree }

func make_(d int) *Tree {
	if d == 0 {
		return &Tree{}
	}
	return &Tree{make_(d - 1), make_(d - 1)}
}

func (t *Tree) count() int {
	if t.l == nil {
		return 1
	}
	return 1 + t.l.count() + t.r.count()
}

func main() {
	long := make_(16)
	sum := 0
	for i := 0; i < 100; i++ {
		sum += make_(12).count()
	}
	println(sum, long.count())
}
