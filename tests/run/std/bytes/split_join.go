package main

import "bytes"

func show(ps [][]byte) {
	print(len(ps), ":")
	for _, p := range ps {
		print("[", string(p), "]")
	}
	println()
}

func main() {
	show(bytes.Split([]byte("a,b,c"), []byte(",")))
	show(bytes.Split([]byte("abc"), nil))
	show(bytes.SplitN([]byte("a,b,c"), []byte(","), 2))
	show(bytes.Fields([]byte("  a  b c ")))
	show(bytes.SplitAfter([]byte("a,b"), []byte(",")))
	println(string(bytes.Join([][]byte{[]byte("x"), []byte("y")}, []byte("--"))))
	println(string(bytes.Join(nil, []byte(","))) == "")
	a, b, ok := bytes.Cut([]byte("k=v"), []byte("="))
	println(string(a), string(b), ok)
}
