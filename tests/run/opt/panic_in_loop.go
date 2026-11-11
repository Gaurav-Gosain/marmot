package main

// A panic in the middle of a loop must leave exactly the elements before it written.
func fill(dst, src []int) (n int) {
	defer func() { recover() }()
	for i := 0; i < len(dst); i++ {
		dst[i] = src[i] * 2
		n = i + 1
	}
	return
}

func divAll(a []int, d []int) (r []int, err any) {
	r = make([]int, len(a))
	defer func() { err = recover() }()
	for i := range a {
		r[i] = a[i] / d[i]
	}
	return
}

func nilLoop(ps []*int) (sum int, err any) {
	defer func() { err = recover() }()
	for _, p := range ps {
		sum += *p
	}
	return
}

func main() {
	dst := make([]int, 100)
	src := make([]int, 37)
	for i := range src {
		src[i] = i + 1
	}
	n := fill(dst, src)
	println(n, dst[36], dst[37], dst[0])
	a := []int{10, 20, 30, 40, 50, 60, 70, 80, 90}
	d := []int{1, 2, 3, 4, 0, 6, 7, 8, 9}
	r, err := divAll(a, d)
	println(r[3], r[4], r[5], err.(error).Error())
	x, y, z := 1, 2, 3
	sum, err := nilLoop([]*int{&x, &y, nil, &z})
	println(sum, err.(error).Error())
	var big [64]int
	idx := 40
	func() {
		defer func() { println(recover().(error).Error()) }()
		for i := 0; i < 64; i++ {
			big[i%idx*2] = i
		}
	}()
	println(big[0], big[78%64])
}
