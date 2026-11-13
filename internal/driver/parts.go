package driver

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

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

// addPart appends a module after the first modules and returns its index
// among the parts; key is its text key for the backend ("" if none).
func (b *build) addPart(m codegen.Module, key string, n int) {
	b.partMu.Lock()
	i := len(b.parts)
	b.parts = append(b.parts, m)
	b.partKeys = append(b.partKeys, key)
	b.partMu.Unlock()
	if b.stream != nil && m.Path != "" {
		b.stream.send(n+i, m.Path, key)
	}
}

// spillParts writes the module of package i and its other parts, if any.
func (b *build) spillParts(i int, m *codegen.Module, parts []codegen.Module, cacheable bool, keys []string, n int) error {
	if len(parts) == 0 {
		return b.spill(i, m, cacheable, keys)
	}
	m.Text = partsHeader + strconv.Itoa(len(parts)+1) + "\n" + m.Text
	for c := range parts {
		pm := &parts[c]
		if cacheable {
			k := partKey(keys[i], c+1)
			if p := b.saveModule(k, pm.Text); p != "" {
				pm.Path, pm.Text = p, ""
				b.addPart(*pm, k+"-"+moduleTextVersion, n)
				continue
			}
			cacheable = false
		}
		b.partMu.Lock()
		j := n + len(b.parts)
		b.parts = append(b.parts, codegen.Module{})
		b.partKeys = append(b.partKeys, "")
		b.partMu.Unlock()
		p := filepath.Join(b.work, fmt.Sprintf("m%03d.mlir", j))
		if err := os.WriteFile(p, appendAliasTypes(nil, pm.Text), 0o644); err != nil {
			return err
		}
		pm.Path, pm.Text = p, ""
		b.partMu.Lock()
		b.parts[j-n] = *pm
		b.partMu.Unlock()
		if b.stream != nil {
			b.stream.send(j, p, "")
		}
	}
	// The first part last: a cached first part means every part is cached.
	return b.spill(i, m, cacheable, keys)
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
