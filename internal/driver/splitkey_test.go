package driver

import (
	"bytes"
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

// A switch that is in the backend's cache key but does not change the code
// must not change the binary. MARMOT_PCLN_SYMFUNCS only acts on pc table
// fragments, which an -O 2 ELF build does not use, so the two builds of a
// pair differ in their keys alone. splitModule once took the suffix of a
// split module without an external definition (most modules after the -wpo
// internalization) from the module's cache key: a key-only change renamed
// the module's locals, moved globals between groups and changed the layout
// (tuios -O 2). By default (MARMOT_SPLIT_NAMEHASH unset) the suffix comes
// from the package, the code and the names of the definitions instead, so
// the two builds give the same binary. MARMOT_SPLIT_NAMEHASH=0 takes the old
// way, and the second case checks that the binaries then differ: the test
// can fail. A small program shows it only with small frontend parts
// (MARMOT_PART_INSTRS) and a small split size (MARMOT_SPLIT): then some split
// modules have no external definition. With the default sizes the binaries
// are the same both ways.
//
// Even so, the old way shows in only about half the pairs, also with one
// backend job: two builds with MARMOT_SPLIT_NAMEHASH=0 and the same key
// can differ too, and the fault does not always change the binary. So each
// case builds up to `pairs` pairs, each pair in a root of its own, and stops
// at the first pair that differs.
func TestSplitKeyOnly(t *testing.T) {
	if testing.Short() || os.Getenv("MARMOT_TEST_SLOW") != "1" {
		t.Skip("builds a program up to 32 times at -O 2 (5 to 10 min): set MARMOT_TEST_SLOW=1 to run it")
	}
	root := os.Getenv("MARMOT_ROOT") // a checkout with an installed build (a worktree has none)
	if root == "" {
		r, err := filepath.Abs("../..")
		if err != nil {
			t.Fatal(err)
		}
		root = r
	}
	if _, err := os.Stat(filepath.Join(root, "build", "bin", "marmot-backend")); err != nil {
		t.Skip("no installed backend")
	}
	t.Setenv("MARMOT_DAEMON", "0")
	t.Setenv("MARMOT_TIER1", "0")
	t.Setenv("MARMOT_SPLIT", "300")        // split every module above 300 instructions
	t.Setenv("MARMOT_PART_INSTRS", "2000") // packages in frontend parts of 2000 instructions
	dir := t.TempDir()
	src := filepath.Join(dir, "prog")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, text := range map[string]string{
		"go.mod":  "module example.com/splitkey\n\ngo 1.24\n",
		"main.go": "package main\n\nimport (\n\t\"cmp\"\n\t\"encoding/json\"\n\t\"maps\"\n\t\"slices\"\n\t\"fmt\"\n\t\"regexp\"\n\t\"sort\"\n\t\"strconv\"\n\t\"strings\"\n)\n\ntype item struct {\n\tName string `json:\"name\"`\n\tN    int    `json:\"n\"`\n}\n\ntype pair[K cmp.Ordered, V any] struct {\n\tk K\n\tv V\n}\n\nfunc sorted[K cmp.Ordered, V any](m map[K]V) []pair[K, V] {\n\tvar ps []pair[K, V]\n\tfor _, k := range slices.Sorted(maps.Keys(m)) {\n\t\tps = append(ps, pair[K, V]{k, m[k]})\n\t}\n\treturn ps\n}\n\nfunc main() {\n\tfmt.Println(sorted(map[string]int{\"b\": 2, \"a\": 1}), sorted(map[int]string{2: \"b\", 1: \"a\"}), sorted(map[float64]bool{1.5: true}))\n\tvar items []item\n\tfor i := range 8 {\n\t\titems = append(items, item{\"x\" + strconv.Itoa(i), i * i})\n\t}\n\tsort.Slice(items, func(a, b int) bool { return items[a].N > items[b].N })\n\tdata, _ := json.Marshal(items)\n\tre := regexp.MustCompile(`\"n\":(\\d+)`)\n\tfmt.Println(strings.Count(string(data), \"name\"), len(re.FindAllString(string(data), -1)))\n}\n",
	} {
		if err := os.WriteFile(filepath.Join(src, name), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	const pairs = 8 // the old way shows in about half the pairs: it misses all 8 in about 1 run of 256
	for _, c := range []struct {
		name     string
		namehash string // "": MARMOT_SPLIT_NAMEHASH unset
		same     bool
	}{
		{"default", "", true},
		{"namehash0", "0", false},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Setenv("MARMOT_SPLIT_NAMEHASH", c.namehash)
			if c.namehash == "" {
				os.Unsetenv("MARMOT_SPLIT_NAMEHASH") // t.Setenv restores it after the test
			}
			for p := range pairs {
				// A root per pair: the MLIR is shared within the pair only.
				r := filepath.Join(dir, c.name+"-root"+strconv.Itoa(p))
				privateRoot(t, root, r)
				var bins [2][]byte
				for k, symfuncs := range []string{"", "1"} {
					t.Setenv("MARMOT_PCLN_SYMFUNCS", symfuncs)
					out := filepath.Join(r, "bin"+string(rune('a'+k)))
					cfg := Config{Root: r, Dir: src, Output: out, OptLevel: "2", Emit: "exe", GC: "precise", Dialect: "llvm",
						Patterns: []string{"."}, Stderr: os.Stderr, TrimPath: true, OptSet: true}
					if _, err := Build(cfg); err != nil {
						t.Fatal(err)
					}
					data, err := os.ReadFile(out)
					if err != nil {
						t.Fatal(err)
					}
					bins[k] = data
				}
				if bytes.Equal(bins[0], bins[1]) {
					continue
				}
				if c.same {
					n := 0
					for n < len(bins[0]) && n < len(bins[1]) && bins[0][n] == bins[1][n] {
						n++
					}
					t.Errorf("pair %d: a key-only switch changed the binary (%d and %d bytes, first difference at byte %d)", p+1, len(bins[0]), len(bins[1]), n)
				}
				return
			}
			if !c.same {
				t.Errorf("with MARMOT_SPLIT_NAMEHASH=%s a key-only switch changed no binary in %d pairs: the program no longer shows the bug, so the default case proves nothing", c.namehash, pairs)
			}
		})
	}
}
