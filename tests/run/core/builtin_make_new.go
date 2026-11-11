package main

type T struct {
	a int
	b string
}

func main() {
	p := new(int)
	*p = 5
	q := new(T)
	q.a = 1
	println(*p, q.a, q.b == "")
	pp := new(*int)
	println(*pp == nil)
	ps := new([]int)
	*ps = append(*ps, 1)
	println(len(*ps))
	arr := new([3]int)
	arr[1] = 4
	println(arr[1], len(arr))
	s := make([]string, 3)
	println(len(s), s[0] == "")
	m := make(map[string][]int)
	m["a"] = append(m["a"], 1)
	println(len(m["a"]))
	n := 0
	z := make([]int, n)
	println(len(z), cap(z), z == nil)
	big := make([]byte, 1<<20)
	println(len(big), big[len(big)-1])
	a, b := new(int), new(int)
	println(a == b, a == a)
	var e1, e2 = new(struct{}), new(struct{})
	_, _ = e1, e2
}
