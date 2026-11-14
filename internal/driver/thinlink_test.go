package driver

import (
	"debug/elf"
	"debug/macho"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestThinLinkGraphAgainstLinker checks the reference graph against what the
// system linker kept: every function of the program's objects that the linked
// binary still has must be live in the graph (no false dead), and the test
// reports how many more the graph keeps. Inputs from a build with -work and
// MARMOT_KEEP_SYMBOLS=1:
//
//	MARMOT_THINLINK_OBJS=<work dir with the objects> MARMOT_THINLINK_LIBS=build/lib/librtcore.a MARMOT_THINLINK_BIN=<binary> go test -run ThinLinkGraph ./internal/driver
func TestThinLinkGraphAgainstLinker(t *testing.T) {
	dir, bin := os.Getenv("MARMOT_THINLINK_OBJS"), os.Getenv("MARMOT_THINLINK_BIN")
	if dir == "" || bin == "" {
		t.Skip("MARMOT_THINLINK_OBJS and MARMOT_THINLINK_BIN not set")
	}
	g := newRefGraph()
	var objs []string
	_ = filepath.Walk(dir, func(p string, fi os.FileInfo, err error) error {
		if err == nil && !fi.IsDir() && strings.HasSuffix(p, ".o") {
			objs = append(objs, p)
		}
		return nil
	})
	// The runtime library (MARMOT_THINLINK_LIBS, ':' separated archives).
	for _, l := range strings.Split(os.Getenv("MARMOT_THINLINK_LIBS"), ":") {
		if l != "" {
			objs = append(objs, l)
		}
	}
	for _, o := range objs {
		if err := g.addObject(o); err != nil {
			t.Fatal(err)
		}
	}
	// main, and what the driver keeps with -u (link1).
	live := g.live("main", "rt_gc_parallel_enable")
	if os.Getenv("TL_DEBUG") != "" {
		roots := g.roots
		g.roots = nil
		t.Logf("roots %d; live from main alone %d", len(roots), len(g.live("main")))
		count := map[string]int{}
		for _, r := range roots {
			g.roots = []string{r}
			count[r] = len(g.live())
		}
		g.roots = roots
		best := ""
		for r, n := range count {
			if best == "" || n > count[best] {
				best = r
			}
		}
		t.Logf("largest root %q reaches %d", best, count[best])
	}
	kept := map[string]bool{}
	if m := os.Getenv("MARMOT_THINLINK_MAP"); m != "" {
		// An ld64 link map (MARMOT_LINKMAP): its symbols section lists every
		// atom the linker kept, exported or not.
		data, err := os.ReadFile(m)
		if err != nil {
			t.Fatal(err)
		}
		in := false
		for _, line := range strings.Split(string(data), "\n") {
			if strings.HasPrefix(line, "# Symbols:") {
				in = true
				continue
			}
			if strings.HasPrefix(line, "# Dead Stripped Symbols:") {
				break
			}
			if !in || strings.HasPrefix(line, "#") {
				continue
			}
			if i := strings.Index(line, "] "); i >= 0 {
				kept[strings.TrimPrefix(strings.TrimSpace(line[i+2:]), "_")] = true
			}
		}
	} else if f, err := elf.Open(bin); err == nil {
		syms, _ := f.Symbols()
		for _, s := range syms {
			if elf.ST_TYPE(s.Info) == elf.STT_FUNC && s.Section != elf.SHN_UNDEF {
				kept[s.Name] = true
			}
		}
	} else if f, err := macho.Open(bin); err == nil && f.Symtab != nil {
		for _, s := range f.Symtab.Syms {
			if s.Sect != 0 && s.Type&0x0e == 0x0e {
				kept[strings.TrimPrefix(s.Name, "_")] = true
			}
		}
	} else {
		t.Fatalf("%s: not an executable", bin)
	}
	// Local symbols are obj#name in the graph and name in the binary.
	plain := func(s string) string {
		if _, n, ok := strings.Cut(s, "#"); ok {
			return n
		}
		return s
	}
	liveByName := map[string]bool{}
	for s := range live {
		liveByName[plain(s)] = true
	}
	var missing []string
	extra, defined := 0, 0
	seen := map[string]bool{}
	for q := range g.refs {
		s := plain(q)
		if strings.HasPrefix(s, "section:") || strings.HasPrefix(s, "ltmp") || seen[s] {
			continue
		}
		seen[s] = true
		defined++
		live := liveByName
		switch {
		case kept[s] && !live[s]:
			missing = append(missing, s)
		case live[s] && !kept[s]:
			extra++
		}
	}
	if os.Getenv("TL_DEBUG") != "" {
		// A path from a root to one symbol the graph keeps and the linker dropped.
		parent := map[string]string{}
		work := append([]string{"main"}, g.roots...)
		for _, r := range work {
			parent[r] = ""
		}
		for len(work) > 0 {
			n := work[0]
			work = work[1:]
			for _, m := range g.refs[n] {
				if _, ok := parent[m]; !ok {
					parent[m] = n
					work = append(work, m)
				}
			}
		}
		shown := 0
		for q := range parent {
			if p := plain(q); !kept[p] && !strings.HasPrefix(p, "section:") && !strings.HasPrefix(p, "ltmp") && shown < 3 {
				shown++
				path := []string{q}
				for c := parent[q]; c != ""; c = parent[c] {
					path = append(path, c)
				}
				t.Logf("kept by the graph only: %v", path)
			}
		}
	}
	t.Logf("%d objects, %d defined symbols, %d live in the graph, %d kept by the linker; graph keeps %d the linker dropped", len(objs), defined, len(live), len(kept), extra)
	if os.Getenv("TL_DEBUG") != "" && len(missing) > 0 {
		// Who points at the first missing symbol, and at those.
		into := map[string][]string{}
		for n, rs := range g.refs {
			for _, r := range rs {
				into[r] = append(into[r], n)
			}
		}
		var who func(string, int)
		who = func(s string, depth int) {
			for _, q := range []string{s} {
				for _, p := range into[q] {
					t.Logf("%*s%s <- %s (live %v)", depth*2, "", q, p, live[plain(p)] || liveByName[plain(p)])
					if depth < 2 {
						who(p, depth+1)
					}
				}
			}
		}
		who(missing[0], 0)
	}
	if len(missing) > 0 {
		if len(missing) > 20 {
			missing = missing[:20]
		}
		t.Errorf("kept by the linker but dead in the graph: %v", missing)
	}
}
