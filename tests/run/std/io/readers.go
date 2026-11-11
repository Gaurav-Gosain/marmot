package main

import (
	"io"
	"strings"
)

func main() {
	r := io.LimitReader(strings.NewReader("0123456789"), 4)
	b, _ := io.ReadAll(r)
	println(string(b))
	mr := io.MultiReader(strings.NewReader("ab"), strings.NewReader(""), strings.NewReader("cd"))
	b, _ = io.ReadAll(mr)
	println(string(b))
	var sb strings.Builder
	n, err := io.Copy(&sb, strings.NewReader("copy me"))
	println(n, err == nil, sb.String())
	n, _ = io.CopyN(&sb, strings.NewReader("123456"), 3)
	println(n, sb.String())
	tr := io.TeeReader(strings.NewReader("tee"), &sb)
	io.ReadAll(tr)
	println(sb.String())
	buf := make([]byte, 4)
	k, err := io.ReadFull(strings.NewReader("ab"), buf)
	println(k, err == io.ErrUnexpectedEOF)
	k, err = io.ReadFull(strings.NewReader(""), buf)
	println(k, err == io.EOF)
	k, err = io.ReadAtLeast(strings.NewReader("abcdef"), buf, 2)
	println(k, err == nil)
}
