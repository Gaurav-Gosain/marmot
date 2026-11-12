// Quicksort with -par: the search loop `for a[i] < p` is the loop's own
// condition, not a bounds check; versioning it scanned a[i:] before every
// entry and made the sort quadratic (minutes instead of a second).
// quicksort on an int slice filled by an LCG.
package main

const n = 3000000

func qsort(a []int) {
	for len(a) > 12 {
		p := a[len(a)/2]
		i, j := 0, len(a)-1
		for i <= j {
			for a[i] < p {
				i++
			}
			for a[j] > p {
				j--
			}
			if i <= j {
				a[i], a[j] = a[j], a[i]
				i++
				j--
			}
		}
		if j+1 < len(a)-i {
			qsort(a[:j+1])
			a = a[i:]
		} else {
			qsort(a[i:])
			a = a[:j+1]
		}
	}
	for i := 1; i < len(a); i++ {
		v := a[i]
		j := i - 1
		for j >= 0 && a[j] > v {
			a[j+1] = a[j]
			j--
		}
		a[j+1] = v
	}
}

func main() {
	a := make([]int, n)
	x := uint64(12345)
	for i := range a {
		x = x*6364136223846793005 + 1442695040888963407
		a[i] = int(x >> 33)
	}
	qsort(a)
	sum := 0
	for i := range a {
		if i > 0 && a[i-1] > a[i] {
			println("not sorted")
			return
		}
		sum += a[i] ^ i
	}
	println("quicksort", sum)
}
