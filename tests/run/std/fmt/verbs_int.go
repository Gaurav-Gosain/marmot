package main

import "fmt"

func main() {
	for _, v := range []int{0, 7, -7, 255, 1 << 40} {
		r := rune(v&0x7f + 32)
		fmt.Printf("%d|%5d|%-5d|%05d|%+d|% d|%x|%X|%#x|%o|%#o|%b|%c|%q|%U\n", v, v, v, v, v, v, v, v, v, v, v, v, r, r, rune(v&0xffff))
	}
	fmt.Printf("%d %d %d %d\n", int8(-128), uint8(255), int64(-1<<63), uint64(1<<64-1))
	fmt.Printf("%08.3d|%.0d|%.5d|%x|%X\n", 42, 0, 42, -255, "hi")
	fmt.Printf("%3c|%-3c|%08b|%o\n", 'x', 'y', 5, 64)
	fmt.Printf("%d%%\n", 50)
}
