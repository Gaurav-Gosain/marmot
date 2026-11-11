package main

func main() {
	g := make([][]int, 3)
	for i := range g {
		g[i] = make([]int, 4)
		for j := range g[i] {
			g[i][j] = i * j
		}
	}
	println(g[2][3], len(g), len(g[0]))
	tri := [][]int{{1}, {1, 1}, {1, 2, 1}, {1, 3, 3, 1}}
	sum := 0
	for _, r := range tri {
		for _, v := range r {
			sum += v
		}
	}
	println(sum)
	var arr [3][3]int
	for i := 0; i < 3; i++ {
		arr[i][i] = 1
	}
	b := arr
	b[0][0] = 9
	println(arr[0][0], b[0][0])
}
