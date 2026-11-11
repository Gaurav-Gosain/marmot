package main

func find(a []int, t int) int {
	for i, v := range a {
		if v == t {
			return i
		}
	}
	return -1
}

func firstNeg(a []float64) (i int, ok bool) {
	for i = range a {
		if a[i] < 0 {
			return i, true
		}
	}
	return len(a), false
}

func sideEffects(a []int, log *[]int) {
	for i, v := range a {
		*log = append(*log, v)
		if v > 5 {
			break
		}
		if i > 100 {
			return
		}
	}
}

func main() {
	a := make([]int, 1000)
	for i := range a {
		a[i] = i * 3 % 1001
	}
	println(find(a, 300), find(a, 999), find(a, 5), find(a, 1001))
	f := make([]float64, 100)
	for i := range f {
		f[i] = float64(i)
	}
	f[63] = -1
	f[64] = -2
	println(firstNeg(f))
	f[63] = 0
	f[64] = 0
	println(firstNeg(f))
	var log []int
	sideEffects([]int{1, 2, 9, 3, 4}, &log)
	println(len(log), log[len(log)-1])
	cnt := 0
	for i := 0; i < 1000; i++ {
		if i%7 == 6 {
			continue
		}
		if i > 900 {
			break
		}
		cnt += i
	}
	println(cnt)
outer:
	for i := 0; i < 10; i++ {
		for j := 0; j < 10; j++ {
			if i*j == 42 {
				println(i, j)
				break outer
			}
		}
	}
}
