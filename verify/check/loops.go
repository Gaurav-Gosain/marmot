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
	"strings"
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

// Num is an integer, or "sym" / null (not a known constant).
type Num struct {
	V   int64
	Sym bool
}

func (n *Num) UnmarshalJSON(b []byte) error {
	if string(b) == `"sym"` || string(b) == "null" {
		n.Sym = true
		return nil
	}
	return json.Unmarshal(b, &n.V)
}

// ID is an access id, a number or a string in the report.
type ID string

func (i *ID) UnmarshalJSON(b []byte) error {
	*i = ID(strings.Trim(string(b), `"`))
	return nil
}

type Access struct {
	ID     ID     `json:"id"`
	Base   string `json:"base"`
	Store  bool   `json:"store"`
	Size   int64  `json:"size"`
	Coef   Num    `json:"coef"`
	Lo     Num    `json:"lo"`
	Hi     Num    `json:"hi"`
	Stride *Num   `json:"stride"` // strided: the row stride t
	Count  *Num   `json:"count"`  // strided: the largest iteration index K
	Row    bool   `json:"row"`
	Span   *Num   `json:"span"` // hi - lo when constant (lo, hi themselves may be symbolic)
}

type Pair struct {
	A   ID     `json:"a"`
	B   ID     `json:"b"`
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

// CheckLoop returns the problems found in one loop's report, and the claims it cannot
// evaluate (symbolic offsets: proved by ScalarEvolution in the pass, not re-checkable here).
func CheckLoop(r LoopReport) (bad, unverified []string) {
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
	acc := map[ID]Access{}
	for _, a := range r.Accesses {
		acc[a.ID] = a
	}
	covered := map[[2]ID]bool{}
	private := map[ID]bool{}
	for _, p := range r.Pairs {
		if p.Why == "private" {
			private[p.A], private[p.B] = true, true
		}
	}
	for _, p := range r.Pairs {
		a, okA := acc[p.A]
		b, okB := acc[p.B]
		if !okA || !okB {
			bad = append(bad, fmt.Sprintf("pair %s/%s names an unknown access", p.A, p.B))
			continue
		}
		covered[[2]ID{p.A, p.B}] = true
		covered[[2]ID{p.B, p.A}] = true
		switch p.Why {
		case "window":
			if why := windowOK(a, b); strings.HasPrefix(why, "symbolic") {
				unverified = append(unverified, fmt.Sprintf("pair %s/%s window: %s", p.A, p.B, why))
			} else if why != "" {
				bad = append(bad, fmt.Sprintf("pair %s/%s window: %s", p.A, p.B, why))
			}
		case "strided":
			if why := stridedOK(a, b); strings.HasPrefix(why, "symbolic") || strings.HasPrefix(why, "missing") {
				unverified = append(unverified, fmt.Sprintf("pair %s/%s strided: %s", p.A, p.B, why))
			} else if why != "" {
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
			if a.ID != b.ID && !covered[[2]ID{a.ID, b.ID}] {
				bad = append(bad, fmt.Sprintf("store %s and access %s: no reason", a.ID, b.ID))
			}
		}
		// a store against itself in other iterations: a window of itself, unless the array is
		// privatized (each chunk writes its own copy)
		if !covered[[2]ID{a.ID, a.ID}] && !private[a.ID] {
			if why := windowOK(a, a); why != "" && stridedOK(a, a) != "" {
				bad = append(bad, fmt.Sprintf("store %s against itself: no reason (%s)", a.ID, why))
			}
		}
	}
	return bad, unverified
}

// windowOK: RuleP.windows_disjoint, hi_X + size_X - lo_Y <= |coef| and the converse.
func windowOK(x, y Access) string {
	if x.Base != y.Base {
		return "different bases"
	}
	// a store against itself: the condition is hi - lo + size <= |coef|; span = hi - lo
	if x.ID == y.ID && !x.Coef.Sym && x.Coef.V != 0 && x.Span != nil && !x.Span.Sym {
		if x.Span.V+x.Size > abs(x.Coef.V) {
			return fmt.Sprintf("span %d + size %d > |coef| %d", x.Span.V, x.Size, abs(x.Coef.V))
		}
		return ""
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
	if x.Coef != y.Coef || *x.Stride != *y.Stride || x.Lo != y.Lo {
		return "the proved form needs the same start, coef and stride"
	}
	// different sizes: the lemma with size = max(size_x, size_y) covers both accesses
	c, t, K, s := x.Coef.V, x.Stride.V, x.Count.V, max(x.Size, y.Size)
	if !(s > 0 && c >= s && K >= 0 && c*K+s <= t) {
		return fmt.Sprintf("c=%d t=%d K=%d size=%d do not satisfy c >= size and c*K + size <= t", c, t, K, s)
	}
	return ""
}

// CheckLoopsReport reads the report and checks every loop in it.
func CheckLoopsReport(path string) (loops int, bad, unverified []string, err error) {
	fh, err := os.Open(path)
	if err != nil {
		return 0, nil, nil, err
	}
	defer fh.Close()
	dec := json.NewDecoder(bufio.NewReader(fh))
	for dec.More() {
		var r LoopReport
		if err := dec.Decode(&r); err != nil {
			return loops, bad, unverified, err
		}
		loops++
		b, u := CheckLoop(r)
		for _, x := range b {
			bad = append(bad, r.Func+" "+r.Loop+": "+x)
		}
		for _, x := range u {
			unverified = append(unverified, r.Func+" "+r.Loop+": "+x)
		}
	}
	return loops, bad, unverified, nil
}
