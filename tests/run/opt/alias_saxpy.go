package main

// Loops over slices that may alias: a vectorizer must not assume y and x are disjoint.
func saxpy(y, x []float64, a float64) {
	for i := range y {
		y[i] += a * x[i]
	}
}

func shiftAdd(a []int, k int) {
	for i := 0; i+k < len(a); i++ {
		a[i+k] += a[i]
	}
}

func prefix(a []int) {
	for i := 1; i < len(a); i++ {
		a[i] += a[i-1]
	}
}

func backward(a []int) {
	for i := len(a) - 2; i >= 0; i-- {
		a[i] += a[i+1]
	}
}

func main() {
	buf := make([]float64, 20)
	for i := range buf {
		buf[i] = float64(i + 1)
	}
	saxpy(buf[:10], buf[10:], 2)
	println(buf[0], buf[9])
	saxpy(buf[1:10], buf[:9], 1)
	println(buf[1], buf[2], buf[3], buf[9])
	saxpy(buf, buf, 1)
	println(buf[0], buf[19])
	a := make([]int, 17)
	for i := range a {
		a[i] = i + 1
	}
	shiftAdd(a, 1)
	println(a[1], a[2], a[3], a[16])
	for i := range a {
		a[i] = 1
	}
	shiftAdd(a, 3)
	println(a[3], a[6], a[9], a[16])
	for i := range a {
		a[i] = 1
	}
	prefix(a)
	println(a[0], a[7], a[16])
	backward(a)
	println(a[0], a[1], a[16])
}
