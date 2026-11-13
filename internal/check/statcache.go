package check

import (
	"bytes"
	"encoding/gob"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
)

// A run reads the module trees many times: the go list cache compares
// every file's size and time and the head of each changed one, the engine
// hashes the contents of every file of the workspace packages, vuln hashes
// heads. The tree is walked once per run (treeMemo), and file contents and
// heads are hashed once per file version (statCache, kept in the store
// keyed by path, size and modification time).

// treeMemo is the walk of the module trees of one run.
type treeMemo struct {
	once  sync.Once
	all   map[string]string   // path -> size and time
	byMod map[string][]string // module dir -> its paths, sorted
}

func (sc *scope) walkTrees() *treeMemo {
	if sc.tree == nil {
		sc.tree = &treeMemo{}
	}
	t := sc.tree
	t.once.Do(func() {
		t.all = map[string]string{}
		t.byMod = map[string][]string{}
		for _, m := range sc.modules {
			var ps []string
			walkModule(m.Dir, func(p string, info os.FileInfo) {
				t.all[p] = statKey(info)
				ps = append(ps, p)
			})
			sort.Strings(ps)
			t.byMod[m.Dir] = ps
		}
	})
	return t
}

func statKey(info os.FileInfo) string {
	return fmt.Sprintf("%d.%d", info.Size(), info.ModTime().UnixNano())
}

// statCache maps a file version (path, size, time) to the hashes of its
// contents and head.
type statCache struct {
	mu    sync.Mutex
	m     map[string]statEntry
	dirty bool
}

type statEntry struct {
	Stat, Hash, Head string
	Fmt              string // the tool that found the file formatted, or ""
}

func statsKey() string { return newHasher("stat-cache-1").sum() }

var (
	statsMu   sync.Mutex
	statsBySt = map[*store]*statCache{}
)

// stats returns the store's stat cache, loaded on first use.
func (s *store) stats() *statCache {
	statsMu.Lock()
	defer statsMu.Unlock()
	if c := statsBySt[s]; c != nil {
		return c
	}
	c := &statCache{m: map[string]statEntry{}}
	if data := s.get(statsKey()); data != nil {
		if gob.NewDecoder(bytes.NewReader(data)).Decode(&c.m) != nil {
			c.m = map[string]statEntry{}
		}
	}
	statsBySt[s] = c
	return c
}

// saveStats writes the stat cache back when the run added to it. Entries
// of files that no longer exist are dropped.
func (s *store) saveStats() {
	if s == nil {
		return
	}
	statsMu.Lock()
	c := statsBySt[s]
	statsMu.Unlock()
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.dirty {
		return
	}
	for p := range c.m {
		if _, err := os.Stat(p); err != nil {
			delete(c.m, p)
		}
	}
	var buf bytes.Buffer
	if gob.NewEncoder(&buf).Encode(c.m) == nil {
		s.put(statsKey(), buf.Bytes())
	}
	c.dirty = false
}

// entry returns the cached entry of p when its stat is k (computed when k
// is "": callers that walked the tree pass what the walk saw).
func (c *statCache) entry(p, k string) (statEntry, string, bool) {
	if k == "" {
		info, err := os.Stat(p)
		if err != nil {
			return statEntry{}, "", false
		}
		k = statKey(info)
	}
	c.mu.Lock()
	e, ok := c.m[p]
	c.mu.Unlock()
	if ok && e.Stat == k {
		return e, k, true
	}
	return statEntry{Stat: k}, k, false
}

func (c *statCache) set(p string, e statEntry) {
	c.mu.Lock()
	c.m[p] = e
	c.dirty = true
	c.mu.Unlock()
}

// fileHashAt is fileHash(p), computed once per version of the file. A
// stat of "" is read from the file.
func (s *store) fileHashAt(p, stat string) string {
	if s == nil {
		return fileHash(p)
	}
	c := s.stats()
	e, _, ok := c.entry(p, stat)
	if ok && e.Hash != "" {
		return e.Hash
	}
	e.Hash = fileHash(p)
	if e.Stat != "" && e.Hash != "" {
		c.set(p, e)
	}
	return e.Hash
}

// headHashAt is headHash(p), computed once per version of the file. A
// stat of "" is read from the file.
func (s *store) headHashAt(p, stat string) string {
	if s == nil {
		return headHash(p)
	}
	c := s.stats()
	e, _, ok := c.entry(p, stat)
	if ok && e.Head != "" {
		return e.Head
	}
	e.Head = headHash(p)
	if e.Stat != "" && !strings.HasPrefix(e.Head, "error:") {
		c.set(p, e)
	}
	return e.Head
}

// fmtClean reports whether this version of the file was found formatted
// by this tool.
func (s *store) fmtClean(p, stat string) bool {
	if s == nil {
		return false
	}
	e, _, ok := s.stats().entry(p, stat)
	return ok && e.Fmt == toolShort()
}

func (s *store) setFmtClean(p, stat string) {
	if s == nil {
		return
	}
	c := s.stats()
	e, _, ok := c.entry(p, stat)
	if !ok {
		e = statEntry{Stat: stat}
	}
	e.Fmt = toolShort()
	c.set(p, e)
}

func toolShort() string { return toolID()[:12] }
