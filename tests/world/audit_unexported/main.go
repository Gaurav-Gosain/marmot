// Audit (spec, 2026-11-12): unexported methods named m in packages pa and pb
// are distinct methods (the Go spec qualifies unexported names by package).
// Both embeds pa.A and pb.B, so it has both; pb.I must dispatch to pb.B.m.
// marmot picks pa.A.m for the pb.I itab, both for the compile time itab
// (pb.Call(Both{})) and the run time one (v.(pb.I)), at every -O.
package main

import (
	"marmotworld/audit_unexported/pa"
	"marmotworld/audit_unexported/pb"
)

type Both struct {
	pa.A
	pb.B
}

type G[E any] struct{ v E }

func (g G[E]) Name() string { return "G" }

type P struct{ n int }

func (p *P) Name() string {
	if p == nil {
		return "nil P"
	}
	return "P"
}

type Inner struct{}

func (*Inner) Name() string { return "inner" }

type Outer struct{ *Inner }

type V int

func (V) Name() string { return "V" }

func main() {
	println(pa.Call(Both{}), pb.Call(Both{}))
	println(pb.CallAny(Both{}), pa.CallAny(Both{}))
	println(pa.Call(pa.A{}), pb.Call(pb.B{}))
	var np *P
	println(pa.Names([]pa.K{G[int]{}, G[string]{}, np, &P{}, V(1), Outer{&Inner{}}}))
	func() {
		defer func() { println("recovered:", recover() != nil) }()
		var k pa.K
		println(k.Name())
	}()
	func() {
		defer func() { println("recovered nil embedded:", recover() != nil) }()
		var k pa.K = Outer{}
		println(k.Name())
	}()
}
