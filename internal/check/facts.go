// The fact set and its encoding follow golang.org/x/tools/internal/facts
// (Copyright 2018 The Go Authors, BSD-style license), which cannot be
// imported from outside x/tools. The encoding is the same: a gob list of
// (package path, object path, fact) sorted by package, object and type.

package check

import (
	"bytes"
	"encoding/gob"
	"fmt"
	"go/types"
	"reflect"
	"sort"
	"sync"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/types/objectpath"
)

// factSet holds the facts visible while analyzing one package: those of
// its dependencies, read from their encoded form, and its own.
type factSet struct {
	pkg *types.Package
	mu  sync.Mutex
	m   map[factKey]analysis.Fact
}

type factKey struct {
	pkg *types.Package
	obj types.Object // nil for package facts
	t   reflect.Type
}

func (s *factSet) importObjectFact(obj types.Object, ptr analysis.Fact) bool {
	if obj == nil {
		panic("nil object")
	}
	k := factKey{pkg: obj.Pkg(), obj: obj, t: reflect.TypeOf(ptr)}
	s.mu.Lock()
	defer s.mu.Unlock()
	if v, ok := s.m[k]; ok {
		reflect.ValueOf(ptr).Elem().Set(reflect.ValueOf(v).Elem())
		return true
	}
	return false
}

func (s *factSet) exportObjectFact(obj types.Object, fact analysis.Fact) {
	if obj.Pkg() != s.pkg {
		panic(fmt.Sprintf("in package %s: ExportObjectFact(%s, %T): fact on an object of another package", s.pkg, obj, fact))
	}
	k := factKey{pkg: obj.Pkg(), obj: obj, t: reflect.TypeOf(fact)}
	s.mu.Lock()
	s.m[k] = fact
	s.mu.Unlock()
}

func (s *factSet) allObjectFacts(filter map[reflect.Type]bool) []analysis.ObjectFact {
	var out []analysis.ObjectFact
	s.mu.Lock()
	for k, v := range s.m {
		if k.obj != nil && filter[k.t] {
			out = append(out, analysis.ObjectFact{Object: k.obj, Fact: v})
		}
	}
	s.mu.Unlock()
	return out
}

func (s *factSet) importPackageFact(pkg *types.Package, ptr analysis.Fact) bool {
	if pkg == nil {
		panic("nil package")
	}
	k := factKey{pkg: pkg, t: reflect.TypeOf(ptr)}
	s.mu.Lock()
	defer s.mu.Unlock()
	if v, ok := s.m[k]; ok {
		reflect.ValueOf(ptr).Elem().Set(reflect.ValueOf(v).Elem())
		return true
	}
	return false
}

func (s *factSet) exportPackageFact(fact analysis.Fact) {
	k := factKey{pkg: s.pkg, t: reflect.TypeOf(fact)}
	s.mu.Lock()
	s.m[k] = fact
	s.mu.Unlock()
}

func (s *factSet) allPackageFacts(filter map[reflect.Type]bool) []analysis.PackageFact {
	var out []analysis.PackageFact
	s.mu.Lock()
	for k, v := range s.m {
		if k.obj == nil && filter[k.t] {
			out = append(out, analysis.PackageFact{Package: k.pkg, Fact: v})
		}
	}
	s.mu.Unlock()
	return out
}

type gobFact struct {
	PkgPath string
	Object  objectpath.Path
	Fact    analysis.Fact
}

// decodeFacts builds the fact set of pkg from the encoded facts of its
// direct imports. getPackage finds any package (perhaps indirect) that the
// type checker loaded.
func decodeFacts(pkg *types.Package, getPackage func(string) *types.Package, read func(path string) []byte) (*factSet, error) {
	m := map[factKey]analysis.Fact{}
	for _, imp := range pkg.Imports() {
		data := read(imp.Path())
		if len(data) == 0 {
			continue
		}
		var gfs []gobFact
		if err := gob.NewDecoder(bytes.NewReader(data)).Decode(&gfs); err != nil {
			return nil, fmt.Errorf("decoding facts for %q: %v", imp.Path(), err)
		}
		for _, f := range gfs {
			fp := getPackage(f.PkgPath)
			if fp == nil {
				continue
			}
			k := factKey{pkg: fp, t: reflect.TypeOf(f.Fact)}
			if f.Object != "" {
				obj, err := objectpath.Object(fp, f.Object)
				if err != nil {
					continue
				}
				k.obj = obj
			}
			m[k] = f.Fact
		}
	}
	return &factSet{pkg: pkg, m: m}, nil
}

// encode returns the facts that importers of the package can see: its own
// facts, and facts about imported types and their members that its API
// makes reachable.
func (s *factSet) encode() ([]byte, error) {
	enc := new(objectpath.Encoder)
	var gfs []gobFact
	s.mu.Lock()
	for k, fact := range s.m {
		if k.pkg != s.pkg {
			if k.obj == nil {
				continue
			}
			if _, isType := k.obj.(*types.TypeName); !isType && k.obj.Parent() == k.obj.Pkg().Scope() {
				continue
			}
		}
		var path objectpath.Path
		if k.obj != nil {
			p, err := enc.For(k.obj)
			if err != nil {
				continue
			}
			path = p
		}
		gfs = append(gfs, gobFact{PkgPath: k.pkg.Path(), Object: path, Fact: fact})
	}
	s.mu.Unlock()
	sort.Slice(gfs, func(i, j int) bool {
		x, y := gfs[i], gfs[j]
		if x.PkgPath != y.PkgPath {
			return x.PkgPath < y.PkgPath
		}
		if x.Object != y.Object {
			return x.Object < y.Object
		}
		return reflect.TypeOf(x.Fact).String() < reflect.TypeOf(y.Fact).String()
	})
	if len(gfs) == 0 {
		return nil, nil
	}
	var buf bytes.Buffer
	if err := gob.NewEncoder(&buf).Encode(gfs); err != nil {
		return nil, fmt.Errorf("encoding facts of %s: %v", s.pkg.Path(), err)
	}
	return buf.Bytes(), nil
}
