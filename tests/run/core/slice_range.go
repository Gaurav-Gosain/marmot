package main

func main() {
	s := []int{10, 20, 30}
	for i, v := range s {
		println(i, v)
	}
	for i := range s {
		s[i] *= 2
	}
	println(s[0], s[1], s[2])
	for i, v := range s {
		if i == 0 {
			s = append(s, 99)
			s[1] = 1000
		}
		println(i, v)
	}
	println(len(s))
	var n []int
	for range n {
		println("never")
	}
	arr := [3]int{1, 2, 3}
	for i, v := range arr {
		arr[2] = 100
		println(i, v)
	}
	for i, v := range &arr {
		arr[2] = 200
		println(i, v)
	}
}
