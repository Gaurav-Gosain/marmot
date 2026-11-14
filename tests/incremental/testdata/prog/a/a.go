// Package a is the dependency the edits change.
package a

// list is an unexported generic type; Box embeds an instance of it, so its
// promoted methods are compiled into importers.
type list[E any] struct{ items []E }

func (l *list[E]) Push(e E) { l.items = append(l.items, e) }

func (l *list[E]) Len() int { return len(l.items) }

// Box is exported and promotes list[int]'s methods.
type Box struct{ list[int] }

// T is a struct whose layout importers depend on.
type T struct {
	X int
	Y string
}

// C and V are read by importers.
const C = 3

var V = 7

var sink *int

// Map is an exported generic function.
func Map[A, B any](xs []A, f func(A) B) []B {
	out := make([]B, 0, grow(len(xs)))
	for _, x := range xs {
		out = append(out, f(x))
	}
	return out
}

func grow(n int) int { return n }

// Keep reads through p without keeping it.
func Keep(p *int) int {
	return *p
}

// Stash keeps p (the "unescapes" edit stops that).
func Stash(p *int) int {
	sink = p
	return *p
}

// Pass returns nil (the "returns" edit returns p: its result then carries it).
func Pass(p *int) *int {
	return nil
}

// MayPanic returns i.
func MayPanic(i int) int {
	return i
}

// Recov returns 1.
func Recov() (r int) {
	return 1
}
