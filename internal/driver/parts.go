package driver

import (
	"bufio"
	"cmp"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"

	"github.com/Gaurav-Gosain/marmot/internal/codegen"
)

// Large packages are generated as several modules (codegen.GenerateParts),
// so that their generation and their backend jobs run in parallel. The
// first part's module is the package's module; the others come after every
// package's first module. In the MLIR cache, part c of the package with key
// k has key partKey(k, c), and the first part's text starts with a line
// "// marmot parts N".
//
//	MARMOT_PART_INSTRS  SSA instructions per part (default 40000; 0: no parts)

const partsHeader = "// marmot parts "

// partInstrs is the size of a part in SSA instructions, or 0.
func (b *build) partInstrs() int {
	if b.wpo() || b.cfg.Dialect == "go" || (b.cfg.Emit != "exe" && b.cfg.Emit != "obj") {
		return 0
	}
	if v, err := strconv.Atoi(os.Getenv("MARMOT_PART_INSTRS")); err == nil {
		return v
	}
	return 40000
}

func partKey(k string, c int) string { return fmt.Sprintf("%s-p%d", k, c) }

// A partID names a part: part c (from 1) of the package with index pkg.
// The instances module (home.go) comes after every part.
type partID struct{ pkg, part int }

var instancesPart = partID{math.MaxInt, 0}

// addPart appends a module after the first modules and returns its index
// among the parts; key is its text key for the backend ("" if none).
func (b *build) addPart(m codegen.Module, key string, n int, id partID) {
	b.partMu.Lock()
	i := len(b.parts)
	b.parts = append(b.parts, m)
	b.partKeys = append(b.partKeys, key)
	b.partIDs = append(b.partIDs, id)
	b.partMu.Unlock()
	if b.stream != nil && m.Path != "" {
		b.stream.send(n+i, m.Path, key)
	}
}

// spillParts writes the module of package i and its other parts, if any.
// Parts that spillPart wrote already (GenerateParts calls it as each part is
// done) have a Path and no text, and are left as they are.
// failed is shared with those spillPart calls.
func (b *build) spillParts(i int, m *codegen.Module, parts []codegen.Module, cacheable bool, failed *atomic.Bool, keys []string, n int) error {
	if len(parts) == 0 {
		return b.spill(i, m, cacheable, keys)
	}
	m.Text.Prepend(partsHeader + strconv.Itoa(len(parts)+1) + "\n")
	// Register the parts in part order, as before parts were written early: the
	// order of b.parts reaches the output (pc table, relocations), so it must
	// not follow the order in which the parts' generation finished.
	for c := range parts {
		if err := b.registerPart(i, c+1, &parts[c], cacheable, failed, keys, n); err != nil {
			return err
		}
	}
	// The first part last: a cached first part means every part is cached.
	return b.spill(i, m, cacheable && !failed.Load(), keys)
}

// spillPart writes part c (from 1) of package i as soon as it is generated and
// drops its text: to the MLIR cache when the package is cacheable and no part
// failed to save there (failed), else to a file of its own in the work
// directory. A failed save sets failed: later parts, and the first part, then
// go to the work directory. registerPart adds the part to the build later, in
// part order.
func (b *build) spillPart(i, c int, pm *codegen.Module, cacheable bool, failed *atomic.Bool, keys []string, n int) error {
	if cacheable && !failed.Load() {
		if p := b.saveModule(partKey(keys[i], c), pm.Text.Segments()); p != "" {
			pm.Path, pm.Text = p, nil
			return nil
		}
		failed.Store(true)
	}
	p := filepath.Join(b.work, fmt.Sprintf("p%03d-%d.mlir", i, c))
	if err := writeModuleFile(p, nil, pm.Text.Segments()); err != nil {
		return err
	}
	pm.Path, pm.Text = p, nil
	return nil
}

// registerPart adds part c (from 1) of package i to the build's modules, writing
// it first if spillPart has not: a part in the MLIR cache with its key, else as
// the work directory's next m%03d.mlir (a part spillPart wrote there is renamed).
func (b *build) registerPart(i, c int, pm *codegen.Module, cacheable bool, failed *atomic.Bool, keys []string, n int) error {
	if pm.Text != nil {
		if err := b.spillPart(i, c, pm, cacheable, failed, keys, n); err != nil {
			return err
		}
	}
	if filepath.Dir(pm.Path) != b.work {
		k := partKey(keys[i], c)
		b.addPart(*pm, k+"-"+moduleTextVersion, n, partID{i, c})
		return nil
	}
	b.partMu.Lock()
	j := n + len(b.parts)
	b.parts = append(b.parts, codegen.Module{})
	b.partKeys = append(b.partKeys, "")
	b.partIDs = append(b.partIDs, partID{i, c})
	b.partMu.Unlock()
	p := filepath.Join(b.work, fmt.Sprintf("m%03d.mlir", j))
	if err := os.Rename(pm.Path, p); err != nil {
		return err
	}
	pm.Path = p
	b.partMu.Lock()
	b.parts[j-n] = *pm
	b.partMu.Unlock()
	if b.stream != nil {
		b.stream.send(j, p, "")
	}
	return nil
}

// partOrder links parts in package and part order (linkOrder), by default;
// MARMOT_PART_ORDER=0 links them in module number order.
var partOrder = os.Getenv("MARMOT_PART_ORDER") != "0"

// linkOrder puts the objects of the parts (after the n first modules) in
// package and part order. Parts get their module numbers as their
// packages finish generating, in parallel, so that order changes from run
// to run; the link order (and with it the layout of the binary and the pc
// table's fragment list) must not.
func (b *build) linkOrder(objs []string, n int) []string {
	if len(objs) != n+len(b.partIDs) {
		return objs // not one object per module (-emit llvm)
	}
	idx := make([]int, len(b.partIDs))
	for k := range idx {
		idx[k] = k
	}
	slices.SortStableFunc(idx, func(x, y int) int {
		a, c := b.partIDs[x], b.partIDs[y]
		if a.pkg != c.pkg {
			return cmp.Compare(a.pkg, c.pkg)
		}
		return cmp.Compare(a.part, c.part)
	})
	out := slices.Clone(objs[:n])
	for _, k := range idx {
		out = append(out, objs[n+k])
	}
	return out
}

// cachedParts returns the cached MLIR files of the package with key k:
// its module, then its other parts.
func (b *build) cachedParts(k string) ([]string, bool) {
	path, ok := b.cachedModule(k)
	if !ok {
		return nil, false
	}
	nparts := 1
	if f, err := os.Open(path); err == nil {
		line, _ := bufio.NewReader(f).ReadString('\n')
		_ = f.Close() // read only
		if rest, ok := strings.CutPrefix(line, partsHeader); ok {
			nparts, _ = strconv.Atoi(strings.TrimSpace(rest))
		}
	}
	paths := []string{path}
	for c := 1; c < nparts; c++ {
		p, ok := b.cachedModule(partKey(k, c))
		if !ok {
			return nil, false
		}
		paths = append(paths, p)
	}
	return paths, true
}
