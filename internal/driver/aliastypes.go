package driver

import (
	"bufio"
	"bytes"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
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

// writeAliasTypes writes text with its struct types aliased (aliasTypes) to
// w, without building the result in memory: on tuios the copies were 1.7 GB
// of the driver's allocation.
func writeAliasTypes(w io.Writer, text string) error {
	if os.Getenv("MARMOT_NO_TYPEALIAS") != "" {
		_, err := io.WriteString(w, text)
		return err
	}
	const prefix = "!llvm.struct<"
	type span struct{ start, end int }
	var spans []span
	count := map[string]int{}
	inString := false
	for i := 0; i < len(text); i++ {
		c := text[i]
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
		if c != '!' || !strings.HasPrefix(text[i:], prefix) {
			continue
		}
		// The matching '>' of this struct type (nested <...> included).
		depth, j := 0, i+len(prefix)-1
		for ; j < len(text); j++ {
			if text[j] == '<' {
				depth++
			} else if text[j] == '>' {
				depth--
				if depth == 0 {
					break
				}
			} else if text[j] == '"' || text[j] == '\n' {
				j = len(text) // a name or a line break inside: leave it
			}
		}
		if j >= len(text) {
			continue
		}
		spans = append(spans, span{i, j + 1})
		count[text[i:j+1]]++
		i = j
	}
	const minAliasUses = 4
	var types []string
	for t, n := range count {
		if n >= minAliasUses {
			types = append(types, t)
		}
	}
	if len(types) == 0 {
		_, err := io.WriteString(w, text)
		return err
	}
	sort.Strings(types)
	name := make(map[string]string, len(types))
	for k, t := range types {
		name[t] = "!mt" + strconv.Itoa(k)
		if _, err := io.WriteString(w, name[t]+" = "+t+"\n"); err != nil {
			return err
		}
	}
	last := 0
	for _, s := range spans {
		if n, ok := name[text[s.start:s.end]]; ok {
			if _, err := io.WriteString(w, text[last:s.start]); err != nil {
				return err
			}
			if _, err := io.WriteString(w, n); err != nil {
				return err
			}
			last = s.end
		}
	}
	_, err := io.WriteString(w, text[last:])
	return err
}

// writeModuleFile writes head and then text with its struct types aliased to
// path, atomically (a temporary file renamed into place), through a buffer.
func writeModuleFile(path string, head []byte, text string) error {
	f, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp*")
	if err != nil {
		return err
	}
	bw := bufio.NewWriterSize(f, 1<<20)
	_, err = bw.Write(head)
	if err == nil {
		err = writeAliasTypes(bw, text)
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
