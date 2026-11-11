package main

// report.go: the certificate go-bce writes with --go-bce='report=FILE' (BCE.cpp; format in
// docs/passes.md): one line per removed check,
//	<func> TAB <walk index> TAB <rule> [TAB <walk index of the dominating check>]
// where the walk index counts go.bounds_check, go.index and go.nil_check ops in pre-order
// before anything is removed. The checker validates exactly the rule the pass names.

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
)

type Claim struct {
	Rule string
	Idx  int
	Dom  int // -1 when the rule names none
}

func readReport(path string) (map[string][]Claim, error) {
	fh, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer fh.Close()
	out := map[string][]Claim{}
	sc := bufio.NewScanner(fh)
	for sc.Scan() {
		t := strings.Split(sc.Text(), "\t")
		if len(t) < 3 {
			continue
		}
		idx, err := strconv.Atoi(t[1])
		if err != nil {
			return nil, fmt.Errorf("report: bad index in %q", sc.Text())
		}
		c := Claim{Rule: t[2], Idx: idx, Dom: -1}
		if len(t) > 3 {
			if c.Dom, err = strconv.Atoi(t[3]); err != nil {
				return nil, fmt.Errorf("report: bad dominating index in %q", sc.Text())
			}
		}
		out[t[0]] = append(out[t[0]], c)
	}
	return out, sc.Err()
}

// checkClaim validates one claimed removal; "" when it holds.
func checkClaim(f *Func, d *Dom, cs []*Check, cl Claim) string {
	if cl.Idx < 0 || cl.Idx >= len(cs) {
		return "walk index out of range"
	}
	c := cs[cl.Idx]
	switch cl.Rule {
	case "B1":
		if c.Kind != "nil" && b1(f, c) {
			return ""
		}
	case "B3":
		if c.Kind != "nil" {
			if ok, why := b3(f, d, c); ok {
				return ""
			} else {
				return "B3: " + why
			}
		}
	case "N1":
		if c.Kind == "nil" && n1(f, c.P) {
			return ""
		}
	case "B2", "N2":
		if cl.Dom < 0 || cl.Dom >= len(cs) {
			return cl.Rule + ": no dominating check named"
		}
		k := cs[cl.Dom]
		if k.Removed {
			return cl.Rule + ": the dominating check was removed too"
		}
		if !d.properlyDominates(k.Op, c.Op) {
			return cl.Rule + ": the named check does not dominate"
		}
		if cl.Rule == "N2" && c.Kind == "nil" && k.Kind == "nil" && k.P == c.P {
			return ""
		}
		if cl.Rule == "B2" && c.Kind != "nil" && k.Kind != "nil" && sameValue(f, k.I, c.I) && sameValue(f, k.N, c.N) {
			return ""
		}
		return cl.Rule + ": the named check is not of the same values"
	default:
		return "unknown rule " + cl.Rule
	}
	return cl.Rule + " does not hold"
}
