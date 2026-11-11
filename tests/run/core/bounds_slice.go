package main

func try(f func()) {
	defer func() {
		println(recover().(error).Error())
	}()
	f()
}

func main() {
	s := make([]int, 3, 5)
	lo, hi, mx := 2, 6, 7
	try(func() { _ = s[:hi] })
	try(func() { _ = s[lo:1] })
	try(func() { _ = s[4:] })
	try(func() { _ = s[:lo:mx] })
	try(func() { _ = s[hi:] })
	str := "abc"
	try(func() { _ = str[lo:hi] })
	try(func() { _ = str[hi] })
	var arr [4]int
	idx := -1
	try(func() { _ = arr[idx] })
	k := 5
	try(func() { _ = arr[k%8] })
	try(func() { _ = make([]int, idx) })
	try(func() { _ = make([]int, 2, lo-1) })
}
