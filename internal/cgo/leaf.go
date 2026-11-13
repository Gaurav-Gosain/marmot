package cgo

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

// Leaf C functions take the fast path too. A C function that calls nothing
// (after clang inlines what it can) cannot block in the kernel or call back
// into Go, so its call needs no syscall bracketing: the wrapper only marks
// the M as in C. Found by compiling each wrapper file to LLVM IR and
// following its calls; anything not understood counts as a call.

var (
	reDefine = regexp.MustCompile(`(?m)^define [^@]*@("?[\w.$]+"?)\(`)
	reCall   = regexp.MustCompile(`(^|[\s=])(call|invoke|callbr)\s`)
	reCallee = regexp.MustCompile(`@("(?:[^"\\]|\\.)*"|[\w.$-]+)\(`)
)

// fastLeaves switches the bracketed wrappers of leaf functions in the
// .cgo2.c files of dir to the fast path. Failures leave the wrappers as
// they are.
func fastLeaves(dir, cc string, flags []string) {
	files, _ := filepath.Glob(filepath.Join(dir, "*.cgo2.c"))
	for _, file := range files {
		args := append([]string{"-S", "-emit-llvm", "-O2", "-w", "-o", "-"}, flags...)
		args = append(args, file)
		out, err := exec.Command(cc, args...).Output()
		if err != nil {
			continue
		}
		leaves := leafFunctions(string(out))
		data, err := os.ReadFile(file)
		if err != nil {
			continue
		}
		c := reBody.ReplaceAllStringFunc(string(data), func(body string) string {
			m := reBody.FindStringSubmatch(body)
			if m[2] != "Cfunc" || !leaves[m[1]] {
				return body
			}
			body = strings.Replace(body, "rt_cgo_enter();", fastEnter, 1)
			return strings.Replace(body, "rt_cgo_exit();", fastExit, 1)
		})
		if c != string(data) {
			if err := os.WriteFile(file, []byte(c), 0o644); err != nil {
				continue
			}
		}
	}
}

// leafFunctions returns the functions of an LLVM IR module that make no
// calls other than to the runtime's cgo entry and exit, to LLVM
// intrinsics, and to other such functions of the module.
func leafFunctions(ir string) map[string]bool {
	calls := map[string][]string{} // function -> callees ("" for an indirect call)
	for _, chunk := range strings.Split(ir, "\ndefine ")[1:] {
		m := reDefine.FindStringSubmatch("define " + chunk)
		if m == nil {
			continue
		}
		name := strings.Trim(m[1], `"`)
		end := strings.Index(chunk, "\n}")
		if end < 0 {
			continue
		}
		body := chunk[:end]
		var callees []string
		for _, line := range strings.Split(body, "\n") {
			if !reCall.MatchString(line) {
				continue
			}
			// A call whose callee is not a plain @name (indirect, or not
			// understood) counts as an unknown call.
			callee := ""
			if cm := reCallee.FindStringSubmatch(line); cm != nil && !strings.HasPrefix(cm[1], `"`) {
				callee = cm[1]
			}
			callees = append(callees, callee)
		}
		if bytes.Contains([]byte(body), []byte(" asm ")) {
			callees = append(callees, "") // inline assembly may be a system call
		}
		calls[name] = callees
	}
	leaf := map[string]bool{}
	var visit func(string, map[string]bool) bool
	visit = func(f string, seen map[string]bool) bool {
		if v, ok := leaf[f]; ok {
			return v
		}
		if seen[f] {
			return false // recursion: not worth proving
		}
		seen[f] = true
		callees, defined := calls[f]
		if !defined {
			return false
		}
		ok := true
		for _, c := range callees {
			switch {
			case strings.HasPrefix(c, "llvm.") && !strings.HasPrefix(c, "llvm.trap"):
			case c == "rt_cgo_enter" || c == "rt_cgo_exit" || c == "rt_cgo_fast_enter" || c == "rt_cgo_fast_exit":
			case c != "" && visit(c, seen):
			default:
				ok = false
			}
		}
		leaf[f] = ok
		return ok
	}
	for f := range calls {
		visit(f, map[string]bool{})
	}
	return leaf
}
