package main

import (
	"io"
	"strings"
)

type zeros struct{}

func (zeros) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 0
	}
	return len(p), nil
}

func main() {
	n, err := io.Copy(io.Discard, io.LimitReader(zeros{}, 3_000_000))
	println(n, err == nil)
	b, _ := io.ReadAll(io.LimitReader(strings.NewReader(strings.Repeat("x", 100000)), 70000))
	println(len(b))
	var sb strings.Builder
	n, _ = io.CopyBuffer(&sb, strings.NewReader("buffered"), make([]byte, 3))
	println(n, sb.String())
}
