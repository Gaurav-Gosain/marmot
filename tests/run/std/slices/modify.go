package main

import "slices"

func pr(s []int) {
	for _, v := range s {
		print(v, " ")
	}
	println("|", len(s))
}

func main() {
	a := []int{1, 2, 3, 4, 5}
	a = slices.Insert(a, 2, 10, 11)
	pr(a)
	a = slices.Delete(a, 1, 3)
	pr(a)
	a = slices.Replace(a, 0, 2, 7)
	pr(a)
	slices.Reverse(a)
	pr(a)
	a = slices.DeleteFunc(a, func(x int) bool { return x%2 == 1 })
	pr(a)
	a = slices.Grow(a, 10)
	println(cap(a) >= len(a)+10)
	a = slices.Clip(a)
	println(cap(a) == len(a))
	b := slices.Concat([]int{1}, []int{2, 3}, nil, []int{4})
	pr(b)
	pr(slices.Repeat([]int{1, 2}, 3))
}
