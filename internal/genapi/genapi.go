// Package genapi tells whether a package's API exposes its generic code.
//
// Other packages can compile instances of a package's generic functions and
// methods only through its API: an exported generic function or type, or an
// instance of one of its generic types that its exported declarations reach
// (a field, an embedded type whose methods are promoted, a signature).
// Separate compilation keys a module by the source of exactly those
// packages among its dependencies, besides their API.
package genapi

import "go/types"

// Exposed reports whether pkg's API exposes generic code of pkg.
func Exposed(pkg *types.Package) bool {
	w := walker{pkg: pkg, seen: map[types.Type]bool{}}
	scope := pkg.Scope()
	for _, name := range scope.Names() {
		obj := scope.Lookup(name)
		if !obj.Exported() {
			continue
		}
		if fn, ok := obj.(*types.Func); ok && fn.Type().(*types.Signature).TypeParams().Len() > 0 {
			return true
		}
		if w.typ(obj.Type()) {
			return true
		}
	}
	return false
}

type walker struct {
	pkg  *types.Package
	seen map[types.Type]bool
}

// typ reports whether t reaches a generic type of w.pkg (generic itself or
// an instance), through its structure and, for named types of w.pkg, their
// methods.
func (w *walker) typ(t types.Type) bool {
	if t == nil || w.seen[t] {
		return false
	}
	w.seen[t] = true
	switch t := t.(type) {
	case *types.Named:
		if t.Obj().Pkg() != w.pkg {
			// Another package's type: its generic code is that package's.
			// Type arguments may still be types of w.pkg.
			for a := range t.TypeArgs().Types() {
				if w.typ(a) {
					return true
				}
			}
			return false
		}
		if t.TypeParams().Len() > 0 || t.TypeArgs().Len() > 0 || t.Origin().TypeParams().Len() > 0 {
			return true
		}
		if w.typ(t.Underlying()) {
			return true
		}
		for i := 0; i < t.NumMethods(); i++ {
			if w.typ(t.Method(i).Type()) {
				return true
			}
		}
	case *types.Alias:
		return w.typ(types.Unalias(t))
	case *types.Pointer:
		return w.typ(t.Elem())
	case *types.Slice:
		return w.typ(t.Elem())
	case *types.Array:
		return w.typ(t.Elem())
	case *types.Map:
		return w.typ(t.Key()) || w.typ(t.Elem())
	case *types.Chan:
		return w.typ(t.Elem())
	case *types.Struct:
		for i := 0; i < t.NumFields(); i++ {
			if w.typ(t.Field(i).Type()) {
				return true
			}
		}
	case *types.Interface:
		for i := 0; i < t.NumEmbeddeds(); i++ {
			if w.typ(t.EmbeddedType(i)) {
				return true
			}
		}
		for i := 0; i < t.NumExplicitMethods(); i++ {
			if w.typ(t.ExplicitMethod(i).Type()) {
				return true
			}
		}
	case *types.Signature:
		if t.TypeParams().Len() > 0 {
			return true
		}
		return w.typ(t.Params()) || w.typ(t.Results())
	case *types.Tuple:
		for i := 0; i < t.Len(); i++ {
			if w.typ(t.At(i).Type()) {
				return true
			}
		}
	case *types.TypeParam:
		return true
	}
	return false
}
