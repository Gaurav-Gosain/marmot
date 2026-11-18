package driver

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// Two cold builds of one program give the same binary, with packages split
// in many parts (MARMOT_PART_INSTRS small): parts get their module numbers
// as their packages finish generating, in parallel, and the link must not
// follow that order (linkOrder; MARMOT_PART_ORDER=0 turns it off).
func TestReproducibleBuild(t *testing.T) {
	if testing.Short() {
		t.Skip("builds four programs")
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
	t.Setenv("MARMOT_PART_INSTRS", "300")
	save := partOrder
	partOrder = true
	defer func() { partOrder = save }()
	t.Setenv("MARMOT_DAEMON", "0")
	t.Setenv("MARMOT_TIER1", "0")
	dir := t.TempDir()
	src := filepath.Join(dir, "prog")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, text := range map[string]string{
		"go.mod":  "module example.com/repro\n\ngo 1.24\n",
		"main.go": "package main\n\nimport (\n\t\"fmt\"\n\t\"strconv\"\n\t\"strings\"\n)\n\nfunc main() {\n\tvar b strings.Builder\n\tfor i := range 5 {\n\t\tb.WriteString(strconv.Itoa(i))\n\t}\n\tfmt.Println(b.String(), fmt.Sprintf(\"%08.3f\", 3.25))\n}\n",
	} {
		if err := os.WriteFile(filepath.Join(src, name), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, mode := range []string{"dev", "O2"} {
		t.Run(mode, func(t *testing.T) {
			var bins [2][]byte
			for k := range bins {
				// A private root per build: an empty cache, the installed tools.
				r := filepath.Join(dir, mode+"-root"+string(rune('a'+k)))
				privateRoot(t, root, r)
				out := filepath.Join(dir, mode+"-bin"+string(rune('a'+k)))
				cfg := Config{Root: r, Dir: src, Output: out, OptLevel: "2", Emit: "exe", GC: "precise", Dialect: "llvm",
					Patterns: []string{"."}, Stderr: os.Stderr, TrimPath: true, OptSet: mode == "O2"}
				if _, err := Build(cfg); err != nil {
					t.Fatal(err)
				}
				data, err := os.ReadFile(out)
				if err != nil {
					t.Fatal(err)
				}
				bins[k] = data
			}
			if !bytes.Equal(bins[0], bins[1]) {
				n := 0
				for n < len(bins[0]) && n < len(bins[1]) && bins[0][n] == bins[1][n] {
					n++
				}
				t.Errorf("two cold builds differ (%d and %d bytes, first difference at byte %d)", len(bins[0]), len(bins[1]), n)
			}
		})
	}
}

// privateRoot makes a marmot root at r that shares root's sources and
// installed tools but has its own (empty) build/cache.
func privateRoot(t *testing.T, root, r string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(r, "build", "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	link := func(from, to string) {
		if err := os.Symlink(from, to); err != nil {
			t.Fatal(err)
		}
	}
	for _, x := range []string{"backend", "lib", "runtime", "tests"} {
		if _, err := os.Stat(filepath.Join(root, x)); err == nil {
			link(filepath.Join(root, x), filepath.Join(r, x))
		}
	}
	link(filepath.Join(root, "build", "lib"), filepath.Join(r, "build", "lib"))
	bins, err := os.ReadDir(filepath.Join(root, "build", "bin"))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range bins {
		link(filepath.Join(root, "build", "bin", e.Name()), filepath.Join(r, "build", "bin", e.Name()))
	}
}

func TestLinkOrder(t *testing.T) {
	b := &build{partIDs: []partID{{3, 2}, instancesPart, {1, 1}, {3, 1}, {1, 2}}}
	got := b.linkOrder([]string{"m0", "m1", "p32", "inst", "p11", "p31", "p12"}, 2)
	want := []string{"m0", "m1", "p11", "p12", "p31", "p32", "inst"}
	if !slices.Equal(got, want) {
		t.Errorf("linkOrder = %q, want %q", got, want)
	}
	if got := b.linkOrder([]string{"m0"}, 2); !slices.Equal(got, []string{"m0"}) {
		t.Errorf("linkOrder of a short list = %q, want it unchanged", got)
	}
}
