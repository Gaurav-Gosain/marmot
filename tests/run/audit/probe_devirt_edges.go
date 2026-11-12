// Audit probe (loops, 2026-11-12): held on HEAD 86cadb6 in dev, -release and
// GC stress. Kept as a guard; see docs/audit-backend.md.
package main

import (
	"fmt"
	"io"
	"strings"
)

type S interface{ Name() string }
type N interface {
	S
	Num() int
}

type P struct{ n int }

func (p *P) Name() string {
	if p == nil {
		return "nil *P"
	}
	return fmt.Sprint("P", p.n)
}
func (p *P) Num() int { return p.n }

type V struct{ n int }

func (v V) Name() string { return fmt.Sprint("V", v.n) }
func (v V) Num() int     { return v.n * 10 }

type Emb struct{ *P }
type EmbI struct{ S }

func try(name string, f func() string) {
	defer func() {
		if r := recover(); r != nil {
			fmt.Println(name, "panic:", r)
		}
	}()
	fmt.Println(name, f())
}

//go:noinline
func flip(b bool) bool { return !b }

func main() {
	try("typed nil ptr", func() string { var p *P; var s S = p; return s.Name() })
	try("typed nil value recv", func() string { var p *V; var s S = p; return s.Name() })
	try("nil iface", func() string { var s S; return s.Name() })
	try("embedded ptr nil", func() string { var s S = Emb{}; return s.Name() })
	try("embedded iface nil", func() string { var s S = EmbI{}; return s.Name() })
	try("embedded iface", func() string { var s S = EmbI{V{3}}; return s.Name() })
	try("iface in iface", func() string { var n N = &P{4}; var s S = n; return s.Name() + fmt.Sprint(n.Num()) })
	try("method value", func() string { var s S = V{5}; f := s.Name; s = V{6}; return f() + s.Name() })
	try("method expr", func() string { f := S.Name; return f(V{7}) })
	try("change type", func() string {
		var s S = V{8}
		if flip(false) {
			s = &P{9}
		}
		return s.Name()
	})
	try("assert ok", func() string {
		var s S = V{1}
		p, ok := s.(*P)
		v, ok2 := s.(V)
		_, ok3 := s.(N)
		return fmt.Sprint(p, ok, v, ok2, ok3)
	})
	try("assert panic", func() string { var s S = V{1}; return s.(*P).Name() })
	try("assert nil", func() string { var s S; return s.(V).Name() })
	try("any assert", func() string {
		var a any = V{2}
		_, isS := a.(S)
		_, isW := a.(io.Writer)
		return fmt.Sprint(isS, isW, a.(S).Name())
	})
	try("std iface", func() string {
		var r io.Reader = strings.NewReader("hey")
		b := make([]byte, 2)
		n, err := r.Read(b)
		return fmt.Sprint(n, err, string(b[:n]))
	})
	try("comparable", func() string {
		var a, b S = V{1}, V{1}
		var c S = &P{1}
		return fmt.Sprint(a == b, a == c)
	})
}
