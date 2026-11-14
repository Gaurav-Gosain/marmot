package driver

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
)

// The thin link's method rule (gc's linker rule, cmd/link deadcode): package
// objects do not define method arrays or itabs; each module's sidecar
// (KEY.methods.json, written by the front end next to its MLIR) describes
// them as rows of symbols, and the thin link defines them in one generated
// object. A row's code cells (ifn, tfn, method expressions) stay non-null
// only when a live call site has the row's method name and signature, or a
// live reflect method lookup reaches it (exported methods). The rest of a
// row (name, method type) is written as given: it keeps nothing alive.

// tlSidecar is one module's sidecar.
type tlSidecar struct {
	Arrays []tlArray `json:"arrays"`
	// Calls: per function symbol, the interface method calls it makes, as
	// [name, signature id]; the name "*" is a reflect method lookup
	// (Method, MethodByName): every exported method of a live type.
	Calls map[string][][2]string `json:"calls"`
}

// tlArray is a global the thin link defines: a method array, a tfn or
// method expression array, or an itab.
type tlArray struct {
	Sym   string   `json:"sym"`
	Elem  []string `json:"elem"`  // column names
	Code  []int    `json:"code"`  // columns nulled in dead rows
	Align int      `json:"align"` // bytes
	Rows  []tlRow  `json:"rows"`
}

type tlRow struct {
	Key      [2]string `json:"key"` // method name, signature id
	Exported bool      `json:"exported"`
	Cells    []string  `json:"cells"` // symbols; "" is null
}

func readSidecar(path string) (*tlSidecar, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var s tlSidecar
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("%s: %v", path, err)
	}
	return &s, nil
}

// methodLive marks the program with the method rule on top of the
// reference graph: it returns the live symbols and, per array, which rows
// keep their code. A row lives when its array is referenced by a live
// symbol and its key is called by a live function (or it is exported and a
// live function does a reflect method lookup); its code cells then become
// live in turn.
func methodLive(g *refGraph, sc []*tlSidecar, extra ...string) (map[string]bool, map[string][]bool) {
	arrays := map[string]*tlArray{}
	calls := map[string][][2]string{}
	for _, s := range sc {
		for i := range s.Arrays {
			arrays[s.Arrays[i].Sym] = &s.Arrays[i]
		}
		for f, c := range s.Calls {
			calls[f] = append(calls[f], c...)
		}
	}
	rows := map[string][]bool{}
	for sym, a := range arrays {
		rows[sym] = make([]bool, len(a.Rows))
	}
	seen := map[string]bool{}
	called := map[[2]string]bool{}
	reflectAll := false
	work := append(append([]string{}, g.roots...), extra...)
	mark := func() {
		for len(work) > 0 {
			s := work[len(work)-1]
			work = work[:len(work)-1]
			if seen[s] {
				continue
			}
			seen[s] = true
			work = append(work, g.refs[s]...)
			for _, c := range calls[s] {
				if c[0] == "*" {
					reflectAll = true
				}
				called[c] = true
			}
		}
	}
	for changed := true; changed; {
		changed = false
		mark()
		for n, targets := range g.support {
			if seen[n] {
				continue
			}
			for _, t := range targets {
				if seen[t] {
					work = append(work, n)
					changed = true
					break
				}
			}
		}
		for sym, a := range arrays {
			if !seen[sym] {
				continue
			}
			for i, r := range a.Rows {
				if rows[sym][i] || !(called[r.Key] || reflectAll && r.Exported) {
					continue
				}
				rows[sym][i] = true
				changed = true
				for _, c := range a.Code {
					if c < len(r.Cells) && r.Cells[c] != "" {
						work = append(work, r.Cells[c])
					}
				}
			}
		}
		// Cells that are not code (names, method types) of a live array
		// are references like any other.
		for sym, a := range arrays {
			if !seen[sym] {
				continue
			}
			code := map[int]bool{}
			for _, c := range a.Code {
				code[c] = true
			}
			for _, r := range a.Rows {
				for j, cell := range r.Cells {
					if !code[j] && cell != "" && !seen[cell] {
						work = append(work, cell)
						changed = true
					}
				}
			}
		}
	}
	return seen, rows
}

// methodArraysIR writes the arrays the thin link defines as LLVM IR: each a
// hidden constant array of pointer rows, dead rows' code cells null. The
// symbols it refers to are declared as external globals (with opaque
// pointers a function and a variable are referred to alike).
func methodArraysIR(sc []*tlSidecar, rows map[string][]bool) string {
	var b strings.Builder
	quote := func(s string) string { return "@\"" + strings.ReplaceAll(s, "\"", "\\22") + "\"" }
	declared := map[string]bool{}
	defined := map[string]bool{}
	for _, s := range sc {
		for _, a := range s.Arrays {
			defined[a.Sym] = true
		}
	}
	var decls []string
	for _, s := range sc {
		for _, a := range s.Arrays {
			live := rows[a.Sym]
			code := map[int]bool{}
			for _, c := range a.Code {
				code[c] = true
			}
			cols := len(a.Elem)
			row := "{" + strings.TrimSuffix(strings.Repeat("ptr, ", cols), ", ") + "}"
			fmt.Fprintf(&b, "%s = hidden constant [%d x %s] [", quote(a.Sym), len(a.Rows), row)
			for i, r := range a.Rows {
				if i > 0 {
					b.WriteString(", ")
				}
				b.WriteString(row + " {")
				for j := 0; j < cols; j++ {
					if j > 0 {
						b.WriteString(", ")
					}
					cell := ""
					if j < len(r.Cells) {
						cell = r.Cells[j]
					}
					if cell == "" || code[j] && (live == nil || !live[i]) {
						b.WriteString("ptr null")
						continue
					}
					b.WriteString("ptr " + quote(cell))
					if !declared[cell] && !defined[cell] {
						declared[cell] = true
						decls = append(decls, cell)
					}
				}
				b.WriteString("}")
			}
			align := a.Align
			if align == 0 {
				align = 8
			}
			fmt.Fprintf(&b, "], align %d\n", align)
		}
	}
	sort.Strings(decls)
	for _, d := range decls {
		fmt.Fprintf(&b, "%s = external hidden global i8\n", quote(d))
	}
	return b.String()
}
