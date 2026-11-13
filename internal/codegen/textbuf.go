package codegen

import (
	"bytes"
	"io"
	"strings"
)

// textBuf is an append-only text buffer made of fixed-size chunks: unlike
// strings.Builder, growing it never copies what was already written. Module
// definitions run to tens of megabytes, where doubling copies dominated the
// allocation (and so the garbage collection) of MLIR generation.
type textBuf struct {
	chunks [][]byte
	n      int
}

const textChunk = 64 << 10

func (t *textBuf) WriteString(s string) (int, error) {
	appendText(t, s)
	return len(s), nil
}

func (t *textBuf) Write(p []byte) (int, error) {
	appendText(t, p)
	return len(p), nil
}

func (t *textBuf) WriteByte(c byte) error {
	k := len(t.chunks) - 1
	if k < 0 || len(t.chunks[k]) == cap(t.chunks[k]) {
		t.chunks = append(t.chunks, make([]byte, 0, textChunk))
		k++
	}
	t.chunks[k] = append(t.chunks[k], c)
	t.n++
	return nil
}

func appendText[S string | []byte](t *textBuf, s S) {
	t.n += len(s)
	for len(s) > 0 {
		k := len(t.chunks) - 1
		if k < 0 || len(t.chunks[k]) == cap(t.chunks[k]) {
			t.chunks = append(t.chunks, make([]byte, 0, textChunk))
			k++
		}
		c := t.chunks[k]
		m := min(len(s), cap(c)-len(c))
		t.chunks[k] = append(c, s[:m]...)
		s = s[m:]
	}
}

// Len is the number of bytes written.
func (t *textBuf) Len() int { return t.n }

// writeTo appends the text to b.
func (t *textBuf) writeTo(b *strings.Builder) {
	for _, c := range t.chunks {
		b.Write(c)
	}
}

// String returns the text.
func (t *textBuf) String() string {
	var b strings.Builder
	b.Grow(t.n)
	t.writeTo(&b)
	return b.String()
}

// initBufs gives f its output buffers, reusing the storage of functions
// already written, and makes the body the current output.
func (m *moduleGen) initBufs(f *funcGen) {
	for _, b := range []*bytes.Buffer{&f.body, &f.pro, &f.cold} {
		if n := len(m.spare); n > 0 {
			*b = *bytes.NewBuffer(m.spare[n-1])
			m.spare = m.spare[:n-1]
		}
	}
	f.b = &f.body
}

// flushBufs writes f's prologue, body and cold code to w and keeps their
// storage for the next function of the module.
func (f *funcGen) flushBufs(w io.Writer) {
	for _, b := range []*bytes.Buffer{&f.pro, &f.body, &f.cold} {
		w.Write(b.Bytes())
		b.Reset()
		if cap(b.Bytes()) > 0 {
			f.m.spare = append(f.m.spare, b.Bytes()[:0])
		}
		*b = bytes.Buffer{}
	}
}
