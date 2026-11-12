package codegen

import "strings"

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
	appendText(t, []byte{c})
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
