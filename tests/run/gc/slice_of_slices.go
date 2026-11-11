package main

func main() {
	rows := make([][]int, 1000)
	for i := range rows {
		rows[i] = make([]int, 100)
		for j := range rows[i] {
			rows[i][j] = i + j
		}
	}
	for r := 0; r < 100; r++ {
		tmp := make([][]int, 500)
		for i := range tmp {
			tmp[i] = make([]int, 200)
		}
	}
	sum := 0
	for _, r := range rows {
		for _, v := range r {
			sum += v
		}
	}
	println(sum)
}
