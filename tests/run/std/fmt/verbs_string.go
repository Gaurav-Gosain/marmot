package main

import "fmt"

func main() {
	s := "héllo\t世\n"
	fmt.Printf("%s|%q|%x|%X|% x|%+q|%#q\n", s, s, s, s, s, s, "plain")
	fmt.Printf("%10s|%-10s|%.3s|%10.2s|%5s|\n", "abc", "abc", "abcdef", "abcdef", "toolongstring")
	fmt.Printf("%s %s\n", []string{"a", "b"}, []byte("bytes"))
	fmt.Printf("%q %q\n", []string{"a b", "c"}, 'x')
	fmt.Printf("%v %v %d\n", []byte("hi"), "str", []byte("hi"))
	fmt.Printf("%t %v %5t|%-7t|\n", true, false, true, false)
	fmt.Printf("%c%c%c %U %#U\n", 72, 0x4e16, 0x1F600, 0x1F600, 'x')
	fmt.Printf("%s %v\n", error(nil), error(nil))
	fmt.Printf("%x %x\n", []byte{1, 0xab, 0xff}, "")
}
