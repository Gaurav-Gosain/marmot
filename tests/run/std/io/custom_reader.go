package main

import "io"

type upper struct{ r io.Reader }

func (u upper) Read(p []byte) (int, error) {
	n, err := u.r.Read(p)
	for i := 0; i < n; i++ {
		if p[i] >= 'a' && p[i] <= 'z' {
			p[i] -= 32
		}
	}
	return n, err
}

type counter struct{ n int }

func (c *counter) Write(p []byte) (int, error) { c.n += len(p); return len(p), nil }

type slowReader struct {
	data string
	pos  int
}

func (s *slowReader) Read(p []byte) (int, error) {
	if s.pos >= len(s.data) {
		return 0, io.EOF
	}
	p[0] = s.data[s.pos]
	s.pos++
	return 1, nil
}

func main() {
	c := &counter{}
	n, _ := io.Copy(c, upper{&slowReader{data: "streaming data"}})
	println(n, c.n)
	b, _ := io.ReadAll(upper{&slowReader{data: "abc"}})
	println(string(b))
	var rw io.ReadWriter
	println(rw == nil)
}
