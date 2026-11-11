package main

type P struct{ a, b int32 }

func main() {
	var keep []*P
	n := 0
	for i := 0; i < 3000000; i++ {
		p := &P{int32(i), 1}
		if i%100000 == 0 {
			keep = append(keep, p)
		}
		n += int(p.b)
	}
	s := int32(0)
	for _, p := range keep {
		s += p.a / 100000
	}
	println(n, len(keep), s)
}
