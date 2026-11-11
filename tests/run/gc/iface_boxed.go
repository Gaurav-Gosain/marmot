package main

type V struct{ a, b, c int }

func main() {
	var xs []interface{}
	for i := 0; i < 10000; i++ {
		xs = append(xs, V{i, i * 2, i * 3})
		xs = append(xs, i)
		xs = append(xs, "s")
	}
	for r := 0; r < 100; r++ {
		_ = make([]byte, 100000)
	}
	t := 0
	for i := 0; i < len(xs); i += 3 {
		v := xs[i].(V)
		t += v.c - v.b
		t += xs[i+1].(int) - xs[i].(V).a
	}
	println(t)
}
