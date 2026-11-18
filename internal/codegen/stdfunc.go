package codegen

import "golang.org/x/tools/go/ssa"

// stdFunc reports whether fn is code of a standard library package: a
// function, method or closure declared there, or a wrapper of such a
// method. Instances of generic functions are not, nor are the closures in
// them: their code runs on the caller's types and data.
func (p *Program) stdFunc(fn *ssa.Function) bool {
	if len(p.opts.StdPkgs) == 0 {
		return false
	}
	for f := fn; f != nil; f = f.Parent() {
		if f.Origin() != nil || len(f.TypeArgs()) > 0 {
			return false
		}
	}
	path := ""
	switch {
	case fn.Pkg != nil:
		path = fn.Pkg.Pkg.Path()
	case fn.Object() != nil && fn.Object().Pkg() != nil:
		path = fn.Object().Pkg().Path()
	default:
		return false
	}
	return p.opts.StdPkgs[path]
}
