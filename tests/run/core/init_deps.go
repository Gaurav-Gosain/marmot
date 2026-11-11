package main

var total = sum(items)
var items = []int{1, 2, 3}
var lookup = map[string]int{"a": base, "b": base + 1}
var base = len(items) * 10

func sum(s []int) int {
	t := 0
	for _, v := range s {
		t += v
	}
	return t
}

var p = &q
var q = 5
var _ = initBlank()

func initBlank() int { println("blank init"); return 0 }

func main() {
	println(total, len(items), lookup["a"], lookup["b"], base, *p)
}
