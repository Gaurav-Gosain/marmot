package main

func main() {
	for i, r := range "aé世😀" {
		println(i, r)
	}
	for i, r := range "a\xffb\xc0\xafc" {
		println(i, r)
	}
	for i, r := range "\xe4\xb8" {
		println(i, r)
	}
	for i := range "xyz" {
		println(i)
	}
	for range "ab" {
		println("tick")
	}
	n := 0
	for _, r := range "" {
		n += int(r)
	}
	println(n)
	for i, r := range "\xed\xa0\x80\xf4\x90\x80\x80" {
		println(i, r)
	}
}
