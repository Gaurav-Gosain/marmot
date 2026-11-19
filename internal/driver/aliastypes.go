package driver

import (
	"bufio"
	"bytes"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
)

// aliasTypes gives the struct types a module text repeats a name: each
// outermost !llvm.struct<...> used at least minAliasUses times is defined once
// at the top ("!mt0 = !llvm.struct<...>") and referred to by name. The MLIR
// parser then parses and uniques each such type once instead of at every use
// (about 20% of parse time on large modules). Types inside string literals
// are left alone.
// MARMOT_NO_TYPEALIAS=1 turns it off.
func aliasTypes(text string) string {
	return string(appendAliasTypes(nil, text))
}

// appendAliasTypes appends text with its struct types aliased (aliasTypes)
// to dst.
func appendAliasTypes(dst []byte, text string) []byte {
	var b bytes.Buffer
	b.Write(dst)
	_ = writeAliasTypes(&b, text)
	return b.Bytes()
}

// writeAliasTypes writes text with its struct types aliased (aliasTypes) to w.
func writeAliasTypes(w io.Writer, text string) error {
	return writeAliasSegs(w, [][]byte{[]byte(text)})
}

// segText is a text made of pieces (codegen.Text), read by offset.
type segText struct {
	segs [][]byte
	off  []int // offset of each piece in the text
	n    int
}

func newSegText(segs [][]byte) *segText {
	t := &segText{}
	for _, s := range segs {
		if len(s) > 0 {
			t.segs = append(t.segs, s)
			t.off = append(t.off, t.n)
			t.n += len(s)
		}
	}
	return t
}

// segCursor reads bytes of a segText by offset; it keeps the piece of the
// last read, so reads near each other cost no search.
type segCursor struct {
	t *segText
	k int
}

func (c *segCursor) at(i int) byte {
	for i >= c.t.off[c.k]+len(c.t.segs[c.k]) {
		c.k++
	}
	for i < c.t.off[c.k] {
		c.k--
	}
	return c.t.segs[c.k][i-c.t.off[c.k]]
}

// piece is the index of the piece that holds offset i (i < t.n).
func (t *segText) piece(i int) int {
	return sort.Search(len(t.segs), func(k int) bool { return t.off[k]+len(t.segs[k]) > i })
}

// slice returns text[i:j]: a part of one piece, or a copy across pieces.
func (t *segText) slice(i, j int) []byte {
	if i >= j {
		return nil
	}
	k := t.piece(i)
	if j <= t.off[k]+len(t.segs[k]) {
		return t.segs[k][i-t.off[k] : j-t.off[k]]
	}
	b := make([]byte, 0, j-i)
	for ; i < j; k++ {
		e := min(j, t.off[k]+len(t.segs[k]))
		b = append(b, t.segs[k][i-t.off[k]:e-t.off[k]]...)
		i = e
	}
	return b
}

// writeRange writes text[i:j] to w.
func (t *segText) writeRange(w io.Writer, i, j int) error {
	if i >= j {
		return nil
	}
	for k := t.piece(i); i < j; k++ {
		e := min(j, t.off[k]+len(t.segs[k]))
		if _, err := w.Write(t.segs[k][i-t.off[k] : e-t.off[k]]); err != nil {
			return err
		}
		i = e
	}
	return nil
}

// writeAliasSegs writes the text made of segs, in order, with its struct
// types aliased (aliasTypes) to w, without joining the pieces or building the
// result in memory: on tuios the copies were 1.7 GB of the driver's allocation.
func writeAliasSegs(w io.Writer, segs [][]byte) error {
	t := newSegText(segs)
	if os.Getenv("MARMOT_NO_TYPEALIAS") != "" {
		return t.writeRange(w, 0, t.n)
	}
	const prefix = "!llvm.struct<"
	type span struct{ start, end int }
	var spans []span
	count := map[string]*int{}
	inString := false
	a := segCursor{t: t}
	hasPrefix := func(i int) bool {
		if i+len(prefix) > t.n {
			return false
		}
		c := a
		for k := 0; k < len(prefix); k++ {
			if c.at(i+k) != prefix[k] {
				return false
			}
		}
		return true
	}
	for i := 0; i < t.n; i++ {
		c := a.at(i)
		if inString {
			switch c {
			case '\\':
				i++
			case '"':
				inString = false
			}
			continue
		}
		if c == '"' {
			inString = true
			continue
		}
		if c != '!' || !hasPrefix(i) {
			continue
		}
		// The matching '>' of this struct type (nested <...> included).
		depth, j := 0, i+len(prefix)-1
		b := a
		for ; j < t.n; j++ {
			if cj := b.at(j); cj == '<' {
				depth++
			} else if cj == '>' {
				depth--
				if depth == 0 {
					break
				}
			} else if cj == '"' || cj == '\n' {
				j = t.n // a name or a line break inside: leave it
			}
		}
		if j >= t.n {
			continue
		}
		spans = append(spans, span{i, j + 1})
		if p := count[string(t.slice(i, j+1))]; p != nil {
			*p++
		} else {
			v := 1
			count[string(t.slice(i, j+1))] = &v
		}
		i = j
	}
	const minAliasUses = 4
	var types []string
	for ty, n := range count {
		if *n >= minAliasUses {
			types = append(types, ty)
		}
	}
	if len(types) == 0 {
		return t.writeRange(w, 0, t.n)
	}
	sort.Strings(types)
	name := make(map[string]string, len(types))
	for k, ty := range types {
		name[ty] = "!mt" + strconv.Itoa(k)
		if _, err := io.WriteString(w, name[ty]+" = "+ty+"\n"); err != nil {
			return err
		}
	}
	last := 0
	for _, s := range spans {
		if n, ok := name[string(t.slice(s.start, s.end))]; ok {
			if err := t.writeRange(w, last, s.start); err != nil {
				return err
			}
			if _, err := io.WriteString(w, n); err != nil {
				return err
			}
			last = s.end
		}
	}
	return t.writeRange(w, last, t.n)
}

// writeModuleFile writes head and then the text made of segs, with its struct
// types aliased, to path, atomically (a temporary file renamed into place),
// through a buffer.
func writeModuleFile(path string, head []byte, segs [][]byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp*")
	if err != nil {
		return err
	}
	bw := bufio.NewWriterSize(f, 1<<20)
	_, err = bw.Write(head)
	if err == nil {
		err = writeAliasSegs(bw, segs)
	}
	if err == nil {
		err = bw.Flush()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Chmod(f.Name(), 0o644)
	}
	if err == nil {
		err = os.Rename(f.Name(), path)
	}
	if err != nil {
		_ = os.Remove(f.Name()) // best effort: the temporary file of a failed write
	}
	return err
}
