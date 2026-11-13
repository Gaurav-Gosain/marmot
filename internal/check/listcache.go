package check

import (
	"bytes"
	"crypto/sha256"
	"encoding/gob"
	"encoding/hex"
	"fmt"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// The go list graph changes only when a file is added or removed, when a
// file's package clause, imports or build constraints change, or when
// go.mod, go.sum or go.work change. A cached graph is reused while the
// directories of the modules list the same files and every file whose size
// or time changed still has the same head. Body edits keep the graph; the
// engine's content hashes pick them up.

type listEntry struct {
	Pkgs  []*listPkg
	Files map[string]fileSig // every file of the module trees
}

type fileSig struct {
	Stat string // size and modification time
	Head string // hash of the package clause, imports and constraints (Go files)
}

// cachedGoList returns the graph for the patterns, from the cache when the
// module trees still match it.
func cachedGoList(st *store, sc *scope, tags, env []string, tests bool) (*graph, bool, error) {
	key := newHasher("golist")
	key.str(toolID(), sc.dir, envKey(tags, hostGOOS(), hostGOARCH()), strings.Join(env, " "))
	key.str(fmt.Sprint(tests))
	key.str(sc.patterns...)
	k := key.sum()
	files := sc.treeFiles()
	if data := st.get(k); data != nil {
		var e listEntry
		if gob.NewDecoder(bytes.NewReader(data)).Decode(&e) == nil && e.matches(files) {
			g := newGraph(e.Pkgs)
			return g, true, nil
		}
	}
	g, err := goList(sc.dir, sc.patterns, tags, env, tests)
	if err != nil {
		return nil, false, err
	}
	e := listEntry{Pkgs: g.pkgs, Files: map[string]fileSig{}}
	for p, stat := range files {
		e.Files[p] = fileSig{Stat: stat, Head: headHash(p)}
	}
	var buf bytes.Buffer
	if gob.NewEncoder(&buf).Encode(&e) == nil {
		st.put(k, buf.Bytes())
	}
	return g, false, nil
}

// matches reports whether the trees still have the same files with the
// same heads, and the generated files the graph names (cgo output in the
// build cache) still exist.
func (e *listEntry) matches(files map[string]string) bool {
	if len(files) != len(e.Files) {
		return false
	}
	for p, stat := range files {
		old, ok := e.Files[p]
		if !ok {
			return false
		}
		if old.Stat != stat && headHash(p) != old.Head {
			return false
		}
	}
	for _, p := range e.Pkgs {
		for _, f := range p.CompiledGoFiles {
			if filepath.IsAbs(f) && !strings.HasPrefix(f, p.Dir) {
				if _, err := os.Stat(f); err != nil {
					return false
				}
			}
		}
	}
	return true
}

// treeFiles lists the files go list reads in the module trees with their
// size and time: Go and assembly sources and go.mod, go.sum and go.work
// files, in every directory go would look at.
func (sc *scope) treeFiles() map[string]string {
	out := map[string]string{}
	for _, m := range sc.modules {
		walkModule(m.Dir, func(p string, info os.FileInfo) {
			out[p] = fmt.Sprintf("%d.%d", info.Size(), info.ModTime().UnixNano())
		})
	}
	if w := workRoot(sc.dir); w != "" {
		for _, n := range []string{"go.work", "go.work.sum"} {
			p := filepath.Join(w, n)
			if info, err := os.Stat(p); err == nil {
				out[p] = fmt.Sprintf("%d.%d", info.Size(), info.ModTime().UnixNano())
			}
		}
	}
	return out
}

var listedExt = map[string]bool{".go": true, ".s": true, ".c": true, ".h": true, ".syso": true, ".cc": true, ".cpp": true, ".m": true}

// walkModule visits the files of a module go list reads, skipping the
// directories go skips (hidden, _ prefixed, testdata) and nested modules.
func walkModule(root string, visit func(string, os.FileInfo)) {
	filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if p == root {
				return nil
			}
			n := d.Name()
			if strings.HasPrefix(n, ".") || strings.HasPrefix(n, "_") || n == "testdata" {
				return filepath.SkipDir
			}
			if _, err := os.Stat(filepath.Join(p, "go.mod")); err == nil {
				return filepath.SkipDir
			}
			return nil
		}
		n := d.Name()
		if n == "go.mod" || n == "go.sum" || listedExt[filepath.Ext(n)] {
			if info, err := d.Info(); err == nil {
				visit(p, info)
			}
		}
		return nil
	})
}

// headHash hashes what go list reads from a Go file: the comments before
// the package clause (build constraints), the package name and the
// imports. Other files hash their contents.
func headHash(path string) string {
	if !strings.HasSuffix(path, ".go") {
		return fileHash(path)
	}
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly|parser.ParseComments)
	if err != nil {
		return "error:" + fileHash(path)
	}
	h := sha256.New()
	for _, cg := range f.Comments {
		if cg.Pos() > f.Package {
			break
		}
		h.Write([]byte(cg.Text()))
	}
	h.Write([]byte(f.Name.Name))
	var imps []string
	for _, im := range f.Imports {
		name := ""
		if im.Name != nil {
			name = im.Name.Name
		}
		imps = append(imps, name+" "+im.Path.Value)
	}
	sort.Strings(imps)
	h.Write([]byte(strings.Join(imps, "\n")))
	// cgo preambles and //go:embed lines are part of the import block's
	// doc comments; include all comments that mention them.
	for _, cg := range f.Comments {
		t := cg.Text()
		if strings.Contains(t, "go:embed") || strings.Contains(t, "#include") || strings.Contains(t, "#cgo") {
			h.Write([]byte(t))
		}
	}
	return hex.EncodeToString(h.Sum(nil))
}

// workRoot is the directory of the go.work above dir, or "".
func workRoot(dir string) string {
	for d := dir; ; d = filepath.Dir(d) {
		if _, err := os.Stat(filepath.Join(d, "go.work")); err == nil {
			return d
		}
		if filepath.Dir(d) == d {
			return ""
		}
	}
}
