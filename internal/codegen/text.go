package codegen

import (
	"io"
	"strings"
)

// Text is the MLIR text of a module in pieces, in order. Generation writes a
// module in chunks (textBuf), and joining them held the text twice while the
// copy was made: on a cold tuios build that was about 600 MB of the driver's
// heap. The driver writes the pieces to the module file as they are.
type Text struct {
	segs [][]byte
	n    int
}

// NewText returns a Text that holds s.
func NewText(s string) *Text {
	t := &Text{}
	t.add([]byte(s))
	return t
}

// add appends b as a piece. It keeps b, not a copy: pass a chunk that is no
// longer written (a finished textBuf chunk) or a fresh slice, never a buffer
// that is reused.
func (t *Text) add(b []byte) {
	if len(b) > 0 {
		t.segs = append(t.segs, b)
		t.n += len(b)
	}
}

// Prepend puts s before the text, as a piece of its own.
func (t *Text) Prepend(s string) {
	if s == "" {
		return
	}
	t.segs = append([][]byte{[]byte(s)}, t.segs...)
	t.n += len(s)
}

// Len is the length of the text in bytes (0 for a nil Text).
func (t *Text) Len() int {
	if t == nil {
		return 0
	}
	return t.n
}

// Segments are the pieces of the text, in order (nil for a nil Text). The
// caller must not change them.
func (t *Text) Segments() [][]byte {
	if t == nil {
		return nil
	}
	return t.segs
}

// WriteTo writes the text to w.
func (t *Text) WriteTo(w io.Writer) (int64, error) {
	var n int64
	for _, s := range t.Segments() {
		k, err := w.Write(s)
		n += int64(k)
		if err != nil {
			return n, err
		}
	}
	return n, nil
}

// String returns the text in one piece.
func (t *Text) String() string {
	var b strings.Builder
	b.Grow(t.Len())
	_, _ = t.WriteTo(&b)
	return b.String()
}
