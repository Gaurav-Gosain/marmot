package main

func main() {
	var all [][]int
	for i := 0; i < 200; i++ {
		var s []int
		for j := 0; j < 5000; j++ {
			s = append(s, j)
		}
		if i%50 == 0 {
			all = append(all, s)
		}
	}
	t := 0
	for _, s := range all {
		t += s[4999] + len(s)
	}
	println(t)
}
