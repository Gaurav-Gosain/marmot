// string concatenation and byte scanning.
package main

const rounds = 900

func main() {
	total := 0
	for r := 0; r < rounds; r++ {
		s := ""
		for i := 0; i < 2000; i++ {
			s += string(rune('a' + (i+r)%26))
		}
		parts := 0
		for i := 0; i < len(s); i++ {
			if s[i] == 'e' || s[i] == 'q' {
				parts++
			}
		}
		t := s + s + s
		for k := 0; k < 200; k++ {
			for i := 0; i < len(t); i++ {
				total += int(t[i]) & 3
			}
		}
		total += parts
	}
	println("strconcat", total)
}
