package driver

import (
	"fmt"
	"os"
	"sort"
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
	if os.Getenv("MARMOT_NO_TYPEALIAS") != "" {
		return text
	}
	const prefix = "!llvm.struct<"
	type span struct{ start, end int }
	var spans []span
	count := map[string]int{}
	inString := false
	for i := 0; i < len(text); i++ {
		c := text[i]
		if inString {
			if c == '\\' {
				i++
			} else if c == '"' {
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
		return text
	}
	sort.Strings(types)
	name := map[string]string{}
	var out strings.Builder
	out.Grow(len(text))
	for k, t := range types {
		name[t] = fmt.Sprintf("!mt%d", k)
		fmt.Fprintf(&out, "%s = %s\n", name[t], t)
	}
	last := 0
	for _, s := range spans {
		if n, ok := name[text[s.start:s.end]]; ok {
			out.WriteString(text[last:s.start])
			out.WriteString(n)
			last = s.end
		}
	}
	out.WriteString(text[last:])
	return out.String()
}
