package main

func main() {
	keep := make([]string, 0, 100)
	for i := 0; i < 100000; i++ {
		s := "item-" + string(rune('a'+i%26)) + string(rune('A'+i/26%26))
		if i%1000 == 0 {
			keep = append(keep, s)
		}
	}
	println(len(keep), keep[0], keep[99])
	b := []byte{}
	for i := 0; i < 100000; i++ {
		b = append(b[:0], "x"...)
		_ = string(b) + "y"
	}
	println(string(b))
}
