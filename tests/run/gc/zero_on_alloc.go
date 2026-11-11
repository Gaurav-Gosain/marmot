package main

func fill() {
	b := make([]byte, 4096)
	for i := range b {
		b[i] = 0xff
	}
}

func main() {
	bad := 0
	for r := 0; r < 2000; r++ {
		fill()
		z := make([]byte, 4096)
		for _, v := range z {
			if v != 0 {
				bad++
			}
		}
		type S struct {
			a *int
			b [10]int
		}
		s := new(S)
		if s.a != nil || s.b[9] != 0 {
			bad++
		}
	}
	println(bad)
}
