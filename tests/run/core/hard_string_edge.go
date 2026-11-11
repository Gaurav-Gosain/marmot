package main

func main() {
	s := "a\x00b"
	println(len(s), s[1], s == "a\x00b", s < "a\x01")
	var empty string
	println(empty[0:0] == "", len(empty[:]))
	b := []byte("héllo")
	for i, c := range string(b) {
		print(i, ":", c, " ")
	}
	println()
	bs := []byte{}
	for r := rune(0); r < 0x300; r += 0x7f {
		bs = append(bs, string(r)...)
	}
	println(len(bs), len([]rune(string(bs))))
	x := "abc"
	y := x[1:2] + x[:1] + x[2:]
	println(y, y == "bac")
	big := make([]byte, 70000)
	for i := range big {
		big[i] = 'a' + byte(i%26)
	}
	str := string(big)
	println(len(str), str[69999], str[26] == 'a', str[65536:65540])
	big[0] = 'Z'
	println(str[0])
	var sum int
	for _, r := range "日本語\xffé" {
		sum += int(r)
	}
	println(sum)
	println("a"+"b" == "ab", ""+"" == "")
	r := []rune("日本")
	r[0] = 'x'
	println(string(r), len(string(r)))
}
