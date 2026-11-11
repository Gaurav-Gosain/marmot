package main

// loops.go: validation of the Rule P report (-loops-report=FILE, Parallelize.cpp): one JSON
// object per parallelized loop with the facts the pass relied on. The checker re-checks the
// parts that are arithmetic or rule membership, against the proved forms in verify/lean:
//   - reductions: integer, op in the accepted set (RuleR.split_eq and the *_ok instances);
//   - every (store, other access) pair has a reason; for "window" the inequality of
//     RuleP.windows_disjoint holds both ways with the same coef; for "strided" the condition
//     of RuleP.strided_disjoint holds.
// It cannot confirm that the numbers describe the IR; reasons "noalias", "runtime", "rows"
// and "private" are taken as stated (the pass's alias analysis or a run time check).

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
)

type LoopReport struct {
	Func     string   `json:"func"`
	Loop     string   `json:"loop"`
	Btc      *int64   `json:"btc"`
	Phis     []Phi    `json:"phis"`
	Accesses []Access `json:"accesses"`
	Pairs    []Pair   `json:"pairs"`
}

type Phi struct {
	Kind  string `json:"kind"`
	Op    string `json:"op"`
	Width int    `json:"width"`
	Float bool   `json:"float"`
}

// Num is an integer or "sym".
type Num struct {
	V   int64
	Sym bool
}

func (n *Num) UnmarshalJSON(b []byte) error {
	if string(b) == `"sym"` {
		n.Sym = true
		return nil
	}
	return json.Unmarshal(b, &n.V)
}

type Access struct {
	ID     string `json:"id"`
	Base   string `json:"base"`
	Store  bool   `json:"store"`
	Size   int64  `json:"size"`
	Coef   Num    `json:"coef"`
	Lo     Num    `json:"lo"`
	Hi     Num    `json:"hi"`
	Stride *Num   `json:"stride"` // strided: the row stride t
	Count  *Num   `json:"count"`  // strided: the largest iteration index K
}

type Pair struct {
	A   string `json:"a"`
	B   string `json:"b"`
	Why string `json:"why"`
}

var redOps = map[string]bool{"add": true, "sub": true, "xor": true, "or": true, "and": true,
	"smin": true, "smax": true, "umin": true, "umax": true}

func abs(x int64) int64 {
	if x < 0 {
		return -x
	}
	return x
}

// CheckLoop returns the problems found in one loop's report.
func CheckLoop(r LoopReport) []string {
	var bad []string
	for _, p := range r.Phis {
		switch p.Kind {
		case "ind", "poly":
		case "red":
			if p.Float || !redOps[p.Op] {
				bad = append(bad, fmt.Sprintf("reduction %q (float %v) is not an accepted integer op", p.Op, p.Float))
			}
		default:
			bad = append(bad, "header value of kind "+p.Kind)
		}
	}
	acc := map[string]Access{}
	for _, a := range r.Accesses {
		acc[a.ID] = a
	}
	covered := map[[2]string]bool{}
	for _, p := range r.Pairs {
		a, okA := acc[p.A]
		b, okB := acc[p.B]
		if !okA || !okB {
			bad = append(bad, fmt.Sprintf("pair %s/%s names an unknown access", p.A, p.B))
			continue
		}
		covered[[2]string{p.A, p.B}] = true
		covered[[2]string{p.B, p.A}] = true
		switch p.Why {
		case "window":
			if why := windowOK(a, b); why != "" {
				bad = append(bad, fmt.Sprintf("pair %s/%s window: %s", p.A, p.B, why))
			}
		case "strided":
			if why := stridedOK(a, b); why != "" {
				bad = append(bad, fmt.Sprintf("pair %s/%s strided: %s", p.A, p.B, why))
			}
		case "noalias", "runtime", "rows", "private":
		default:
			bad = append(bad, fmt.Sprintf("pair %s/%s: unknown reason %q", p.A, p.B, p.Why))
		}
	}
	// clause 5: every store against every other access needs a reason
	for _, a := range r.Accesses {
		if !a.Store {
			continue
		}
		for _, b := range r.Accesses {
			if a.ID != b.ID && !covered[[2]string{a.ID, b.ID}] {
				bad = append(bad, fmt.Sprintf("store %s and access %s: no reason", a.ID, b.ID))
			}
		}
		// a store against itself in other iterations: a window of itself
		if !covered[[2]string{a.ID, a.ID}] {
			if why := windowOK(a, a); why != "" && stridedOK(a, a) != "" {
				bad = append(bad, fmt.Sprintf("store %s against itself: %s", a.ID, why))
			}
		}
	}
	return bad
}

// windowOK: RuleP.windows_disjoint, hi_X + size_X - lo_Y <= |coef| and the converse.
func windowOK(x, y Access) string {
	if x.Base != y.Base {
		return "different bases"
	}
	if x.Coef.Sym || y.Coef.Sym || x.Lo.Sym || x.Hi.Sym || y.Lo.Sym || y.Hi.Sym {
		return "symbolic bounds (needs a run time check)"
	}
	if x.Coef.V != y.Coef.V || x.Coef.V == 0 {
		return "coefficients differ or are zero"
	}
	c := abs(x.Coef.V)
	if x.Hi.V+x.Size-y.Lo.V > c || y.Hi.V+y.Size-x.Lo.V > c {
		return fmt.Sprintf("windows overlap: [%d,%d)+%d and [%d,%d)+%d with |coef| %d", x.Lo.V, x.Hi.V, x.Size, y.Lo.V, y.Hi.V, y.Size, c)
	}
	return ""
}

// stridedOK: RuleP.strided_disjoint, same start, c >= size > 0, c*K + size <= t.
func stridedOK(x, y Access) string {
	if x.Base != y.Base || x.Stride == nil || y.Stride == nil || x.Count == nil {
		return "missing stride or count"
	}
	if x.Coef.Sym || x.Stride.Sym || x.Count.Sym || x.Lo.Sym || y.Lo.Sym {
		return "symbolic values"
	}
	if x.Coef != y.Coef || *x.Stride != *y.Stride || x.Size != y.Size || x.Lo != y.Lo {
		return "the proved form needs the same start, coef, stride and size"
	}
	c, t, K, s := x.Coef.V, x.Stride.V, x.Count.V, x.Size
	if !(s > 0 && c >= s && K >= 0 && c*K+s <= t) {
		return fmt.Sprintf("c=%d t=%d K=%d size=%d do not satisfy c >= size and c*K + size <= t", c, t, K, s)
	}
	return ""
}

// CheckLoopsReport reads the report and checks every loop in it.
func CheckLoopsReport(path string) (loops int, bad []string, err error) {
	fh, err := os.Open(path)
	if err != nil {
		return 0, nil, err
	}
	defer fh.Close()
	dec := json.NewDecoder(bufio.NewReader(fh))
	for dec.More() {
		var r LoopReport
		if err := dec.Decode(&r); err != nil {
			return loops, bad, err
		}
		loops++
		for _, b := range CheckLoop(r) {
			bad = append(bad, r.Func+" "+r.Loop+": "+b)
		}
	}
	return loops, bad, nil
}
