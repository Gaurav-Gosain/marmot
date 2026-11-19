package driver

import (
	"bytes"
	"io"
	"math/rand/v2"
	"os"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// writeAliasTypesRef is writeAliasTypes as it was on one string, before the
// text came in pieces: the reference for TestAliasSegments.
func writeAliasTypesRef(w io.Writer, text string) error {
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

// TestAliasSegments: the text in pieces, split anywhere (inside a struct type,
// a string literal or an escape), gives the same output as the text in one
// string did.
func TestAliasSegments(t *testing.T) {
	s := "!llvm.struct<(ptr, i64)>"
	n := "!llvm.struct<(i64, !llvm.struct<(ptr, ptr)>)>"
	in := s + " first\nmodule {\n" +
		strings.Repeat("  %a = llvm.call @f() : () -> "+s+"\n", 4) +
		strings.Repeat("  %b = llvm.call @g() : () -> "+n+"\n", 5) +
		"  %c = llvm.call @h() : () -> !llvm.struct<(f64)>\n" +
		"  llvm.mlir.global internal @s(\"!llvm.struct<(ptr, i64)>\\\" \\\\\") : !llvm.array<24 x i8>\n" +
		"  %d = llvm.call @k() : () -> !llvm.struct<\"named\", (ptr)>\n" +
		"  %e = llvm.call @k() : () -> !llvm.struct<(ptr,\n i64)>\n" +
		"}\n" + s
	ref := func(text string) string {
		var b bytes.Buffer
		if err := writeAliasTypesRef(&b, text); err != nil {
			t.Fatal(err)
		}
		return b.String()
	}
	segs := func(text string, cuts []int) string {
		var p [][]byte
		last := 0
		for _, c := range cuts {
			p = append(p, []byte(text[last:c]))
			last = c
		}
		p = append(p, []byte(text[last:]))
		var b bytes.Buffer
		if err := writeAliasSegs(&b, p); err != nil {
			t.Fatal(err)
		}
		return b.String()
	}
	for _, text := range []string{in, "", "module {\n}\n", strings.Repeat(s, 4)} {
		want := ref(text)
		if got := segs(text, nil); got != want {
			t.Fatalf("one piece:\n%s\nwant:\n%s", got, want)
		}
		for c := 0; c <= len(text); c++ {
			if got := segs(text, []int{c}); got != want {
				t.Fatalf("cut at %d:\n%s\nwant:\n%s", c, got, want)
			}
		}
		r := rand.New(rand.NewPCG(1, 2))
		for range 2000 {
			cuts := make([]int, r.IntN(12))
			for k := range cuts {
				cuts[k] = r.IntN(len(text) + 1)
			}
			sort.Ints(cuts)
			if got := segs(text, cuts); got != want {
				t.Fatalf("cuts %v:\n%s\nwant:\n%s", cuts, got, want)
			}
		}
	}
	t.Setenv("MARMOT_NO_TYPEALIAS", "1")
	if got := segs(in, []int{3, 50, 51}); got != in {
		t.Errorf("MARMOT_NO_TYPEALIAS: the text changed")
	}
}
