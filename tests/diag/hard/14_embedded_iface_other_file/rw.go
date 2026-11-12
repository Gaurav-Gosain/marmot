package main

type Reader interface{ Read() string }
type Writer interface{ Write(s string) }
type ReadWriter interface {
	Reader
	Writer
}

type Buf struct{ data []string }

func (b *Buf) Write(s string) { b.data = append(b.data, s) }
func (b *Buf) Raed() string {
	if len(b.data) == 0 {
		return ""
	}
	s := b.data[0]
	b.data = b.data[1:]
	return s
}
