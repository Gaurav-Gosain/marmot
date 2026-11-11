// heapsort on an int slice filled by an LCG.
package main

const n = 8000000

func sift(a []int, i, n int) {
	for {
		c := 2*i + 1
		if c >= n {
			return
		}
		if c+1 < n && a[c+1] > a[c] {
			c++
		}
		if a[i] >= a[c] {
			return
		}
		a[i], a[c] = a[c], a[i]
		i = c
	}
}

func main() {
	a := make([]int, n)
	x := uint64(99)
	for i := range a {
		x = x*6364136223846793005 + 1442695040888963407
		a[i] = int(x >> 33)
	}
	for i := n/2 - 1; i >= 0; i-- {
		sift(a, i, n)
	}
	for e := n - 1; e > 0; e-- {
		a[0], a[e] = a[e], a[0]
		sift(a, 0, e)
	}
	sum := 0
	for i := range a {
		if i > 0 && a[i-1] > a[i] {
			println("not sorted")
			return
		}
		sum += a[i] ^ i
	}
	println("heapsort", sum)
}
