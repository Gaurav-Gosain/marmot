package main

// Induction variable overflow and wrap must be preserved by loop transforms.
func main() {
	var i8 int8
	n := 0
	for i8 = 100; i8 > 0; i8 += 10 {
		n++
	}
	println(n, i8)
	var u8 uint8
	n = 0
	for u8 = 250; u8 >= 250; u8++ {
		n++
	}
	println(n, u8)
	var s int32 = 2147483600
	n = 0
	for i := int32(0); i < 100; i++ {
		s += i
		if s < 0 {
			n++
		}
	}
	println(s, n)
	k := 0
	for i := uint(10); i < 20; i -= 3 {
		k++
		if k > 6 {
			break
		}
	}
	println(k)
	total := 0
	for i := 0; i < 10; i++ {
		total += i << (i * 7)
	}
	println(total)
	var acc uint64 = 1
	for i := 0; i < 100; i++ {
		acc = acc*6364136223846793005 + 1442695040888963407
	}
	println(acc)
	for i := int64(1<<63 - 3); i > 0; i++ {
		print(i&7, " ")
	}
	println()
}
