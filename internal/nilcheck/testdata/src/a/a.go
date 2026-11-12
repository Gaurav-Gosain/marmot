package a

import (
	"errors"
	"lib"
)

type T struct{ n int }

func phi(c bool) int {
	var p *T
	if c {
		p = &T{n: 1}
	}
	return p.n // want `p may be nil when dereferenced: it is nil on one path into this use`
}

func phiGuarded(c bool) int {
	var p *T
	if c {
		p = &T{n: 1}
	}
	if p != nil {
		return p.n
	}
	return 0
}

func phiGuardedEq(c bool) int {
	var p *T
	if c {
		p = &T{}
	}
	if p == nil {
		return 0
	}
	return p.n
}

func crossPackage(id int) string {
	u, err := lib.Find(id)
	if err != nil {
		return ""
	}
	return u.Name // want `the result of Find may be nil when dereferenced: lib.Find returns nil for it with a nil error at lib.go:8`
}

func crossChecked(id int) string {
	u, err := lib.Find(id)
	if err != nil || u == nil {
		return ""
	}
	return u.Name
}

func must() string { return lib.Must(1).Name }

func errorReturn() (*T, error) { return nil, errors.New("x") }

func errorPath() int {
	t, err := errorReturn()
	if err != nil {
		return 0
	}
	return t.n
}

func nilMap(c bool) {
	var m map[string]int
	if c {
		m = map[string]int{}
	}
	m["a"] = 1 // want `m may be nil when written to`
}
