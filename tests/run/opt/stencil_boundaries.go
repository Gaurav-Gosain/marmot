package main

func blur(dst, src []float64) {
	for i := 1; i < len(src)-1; i++ {
		dst[i] = (src[i-1] + src[i] + src[i+1]) / 3
	}
}

func blurInPlace(a []float64) {
	for i := 1; i < len(a)-1; i++ {
		a[i] = (a[i-1] + a[i] + a[i+1]) / 3
	}
}

func conv(out, in, k []int) {
	for i := 0; i+len(k) <= len(in); i++ {
		s := 0
		for j := range k {
			s += in[i+j] * k[j]
		}
		out[i] = s
	}
}

func main() {
	src := make([]float64, 25)
	for i := range src {
		src[i] = float64(i * i % 11)
	}
	dst := make([]float64, 25)
	blur(dst, src)
	println(dst[0], dst[1], dst[12], dst[23], dst[24])
	blurInPlace(src)
	println(src[1], src[2], src[12], src[23])
	for _, n := range []int{0, 1, 2, 3} {
		blurInPlace(make([]float64, n))
	}
	in := []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}
	out := make([]int, 10)
	conv(out, in, []int{1, -2, 1})
	conv(out[3:], in, []int{1, 1})
	println(out[0], out[1], out[2], out[3], out[9])
}
