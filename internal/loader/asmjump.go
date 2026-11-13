package loader

import (
	"bufio"
	"fmt"
	"go/ast"
	"os"
	"strings"

	"golang.org/x/tools/go/packages"
)

// Assembly stubs. marmot does not assemble .s files. A package outside the
// standard library (whose assembly lib/src replaces) may declare a
// function without a body and implement it in assembly as a jump to a Go
// function, as golang.org/x/sys/unix does on linux:
//
//	TEXT ·Syscall(SB),NOSPLIT,$0-56
//		JMP	syscall·Syscall(SB)
//
// The loader gives such a declaration a Go body that calls the target
// (an exported function of a package it imports, or any function of the
// same package), through the same source edits as //go:embed. A few raw system
// call stubs of x/sys/unix that are not jumps get bodies by name.

// asmJump is the target of a jump stub: package path ("" for the same
// package) and function name.
type asmJump struct{ pkg, name string }

// rawStubs are x/sys/unix linux functions written as raw system calls in
// assembly: each calls the package syscall function and drops the error.
var rawStubs = map[string]string{
	"SyscallNoError":    "Syscall",
	"RawSyscallNoError": "RawSyscall",
}

// asmJumps reads the jump stubs of p's assembly files.
func asmJumps(p *packages.Package) map[string]asmJump {
	if p.Module == nil {
		return nil // the standard library: lib/src replaces its assembly
	}
	var out map[string]asmJump
	for _, file := range p.OtherFiles {
		if !strings.HasSuffix(file, ".s") {
			continue
		}
		f, err := os.Open(file)
		if err != nil {
			continue
		}
		sc := bufio.NewScanner(f)
		found := map[string]asmJump{}
		text := ""
		for sc.Scan() {
			line := strings.TrimSpace(sc.Text())
			if i := strings.Index(line, "//"); i >= 0 {
				line = strings.TrimSpace(line[:i])
			}
			if line == "" {
				continue
			}
			if strings.HasPrefix(line, "TEXT") {
				text = ""
				fields := strings.Fields(strings.TrimPrefix(line, "TEXT"))
				if len(fields) > 0 && strings.HasPrefix(fields[0], "·") {
					if name, _, ok := strings.Cut(strings.TrimPrefix(fields[0], "·"), "(SB)"); ok {
						text = name
					}
				}
				continue
			}
			if text == "" {
				continue
			}
			// The first instruction of the function decides.
			fn := text
			text = ""
			fields := strings.Fields(line)
			if len(fields) != 2 || fields[0] != "JMP" || !strings.HasSuffix(fields[1], "(SB)") {
				continue
			}
			target := strings.TrimSuffix(fields[1], "(SB)")
			pkg, name, ok := strings.Cut(target, "·")
			if !ok || name == "" || strings.ContainsAny(name, "<>+") {
				continue
			}
			pkg = strings.ReplaceAll(pkg, "∕", "/")
			if pkg != "" && !ast.IsExported(name) {
				continue // unexported in another package: not callable from Go
			}
			if _, ok := p.Imports[pkg]; pkg != "" && !ok {
				continue // the package graph is go list's: no new imports
			}
			found[fn] = asmJump{pkg, name}
		}
		f.Close()
		if sc.Err() != nil {
			continue // unreadable, as when it cannot be opened
		}
		for fn, j := range found {
			if out == nil {
				out = map[string]asmJump{}
			}
			out[fn] = j
		}
	}
	if p.PkgPath == "golang.org/x/sys/unix" && strings.Contains(strings.Join(p.OtherFiles, " "), "asm_linux_") {
		for stub, target := range rawStubs {
			if _, ok := out[stub]; !ok {
				if out == nil {
					out = map[string]asmJump{}
				}
				out[stub] = asmJump{"syscall", target + "\x00drop"}
			}
		}
	}
	return out
}

// asmEdits gives the body-less declarations of f that jumps names a body.
func asmEdits(p *packages.Package, f *ast.File, jumps map[string]asmJump) []embedEditAt {
	if len(jumps) == 0 {
		return nil
	}
	var out []embedEditAt
	aliases := map[string]string{}
	for _, d := range f.Decls {
		fd, ok := d.(*ast.FuncDecl)
		if !ok || fd.Body != nil || fd.Recv != nil {
			continue
		}
		j, ok := jumps[fd.Name.Name]
		if !ok {
			continue
		}
		var args []string
		named := true
		for _, fl := range fd.Type.Params.List {
			if len(fl.Names) == 0 {
				named = false
			}
			for _, n := range fl.Names {
				if n.Name == "_" {
					named = false
				}
				a := n.Name
				if _, ok := fl.Type.(*ast.Ellipsis); ok {
					a += "..."
				}
				args = append(args, a)
			}
		}
		if !named {
			continue
		}
		callee := j.name
		name, drop := strings.CutSuffix(callee, "\x00drop")
		if j.pkg != "" {
			alias, ok := aliases[j.pkg]
			if !ok {
				alias = fmt.Sprintf("zz_marmot_asm%d", len(aliases))
				aliases[j.pkg] = alias
				out = append(out, embedEditAt{p.Fset.Position(f.Name.End()).Offset, fmt.Sprintf("; import %s %q", alias, j.pkg)})
			}
			name = alias + "." + name
		}
		call := name + "(" + strings.Join(args, ", ") + ")"
		body := " { return " + call + " }"
		if fd.Type.Results == nil || fd.Type.Results.NumFields() == 0 {
			body = " { " + call + " }"
		}
		if drop {
			// Named results r1, r2: the call's third result (the error) is dropped.
			res := fd.Type.Results
			if res == nil || res.NumFields() != 2 || len(res.List[0].Names) == 0 {
				continue
			}
			var rn []string
			for _, fl := range res.List {
				for _, n := range fl.Names {
					rn = append(rn, n.Name)
				}
			}
			if len(rn) != 2 {
				continue
			}
			body = fmt.Sprintf(" { %s, %s, _ = %s; return }", rn[0], rn[1], call)
		}
		out = append(out, embedEditAt{p.Fset.Position(fd.End()).Offset, body})
	}
	return out
}
