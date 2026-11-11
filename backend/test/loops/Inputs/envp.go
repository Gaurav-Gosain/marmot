// envp.go: regression for a versioning miscompile (2026-11-12). The loop
// bound n is a value of another loop that was not in LCSSA form; expanding
// the precondition after cloning rewrote the fast copy's use of n to poison,
// so the fast copy ran one iteration. Reported by backend as probe7.
package main

import "unsafe"

//go:linkname rt_envp rt_envp
func rt_envp() unsafe.Pointer

func cstring(p unsafe.Pointer) string {
	if p == nil {
		return ""
	}
	n := 0
	for *(*byte)(unsafe.Add(p, n)) != 0 {
		n++
	}
	b := make([]byte, n)
	copy(b, unsafe.Slice((*byte)(p), n))
	return string(b)
}

func main() {
	envp := rt_envp()
	n := 0
	for *(*unsafe.Pointer)(unsafe.Add(envp, uintptr(n)*8)) != nil {
		n++
	}
	e := make([]string, n)
	for i := 0; i < n; i++ {
		e[i] = cstring(*(*unsafe.Pointer)(unsafe.Add(envp, uintptr(i)*8)))
	}
	println(n, len(e[0]), len(e[1]), len(e[2]))
}
