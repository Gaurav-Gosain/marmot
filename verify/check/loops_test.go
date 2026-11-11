package main

import (
	"encoding/json"
	"testing"
)

func loop(t *testing.T, s string) LoopReport {
	t.Helper()
	var r LoopReport
	if err := json.Unmarshal([]byte(s), &r); err != nil {
		t.Fatal(err)
	}
	return r
}

func TestLoopsReport(t *testing.T) {
	ok := []string{
		// c[i] = a[i] + b[i]: 8-byte windows, coef 8
		`{"func":"f","loop":"bb1","phis":[{"kind":"ind"},{"kind":"red","op":"add","width":64}],
		  "accesses":[{"id":"s","base":"c","store":true,"size":8,"coef":8,"lo":0,"hi":0},
		              {"id":"l","base":"c","store":false,"size":8,"coef":8,"lo":0,"hi":0},
		              {"id":"m","base":"a","store":false,"size":8,"coef":8,"lo":0,"hi":0}],
		  "pairs":[{"a":"s","b":"l","why":"window"},{"a":"s","b":"m","why":"noalias"},{"a":"s","b":"s","why":"window"}]}`,
		// transpose: column stores, c = 8, t = 8*100, K = 99
		`{"func":"g","loop":"bb2","phis":[{"kind":"ind"}],
		  "accesses":[{"id":"s","base":"o","store":true,"size":8,"coef":8,"lo":0,"hi":0,"stride":800,"count":99}],
		  "pairs":[{"a":"s","b":"s","why":"strided"}]}`,
		// symbolic row start, constant span 0 (par.go escape)
		`{"accesses":[{"id":0,"base":"o","store":true,"size":4,"coef":4,"lo":"sym","hi":"sym","span":0}],
		  "pairs":[{"a":0,"b":0,"why":"window"}]}`,
		// symbolic row start, store row[c] and load row[c] relative: dlo = dhi = 0
		`{"accesses":[{"id":0,"base":"o","store":true,"size":4,"coef":4,"lo":"sym","hi":"sym","span":0},
		              {"id":1,"base":"o","store":false,"size":4,"coef":4,"lo":"sym","hi":"sym","span":0}],
		  "pairs":[{"a":0,"b":0,"why":"window"},{"a":0,"b":1,"why":"window","dlo":0,"dhi":0}]}`,
	}
	for _, s := range ok {
		if bad, _ := CheckLoop(loop(t, s)); len(bad) != 0 {
			t.Errorf("rejected a valid report: %v", bad)
		}
	}
	reject := []string{
		// float reduction
		`{"phis":[{"kind":"red","op":"add","float":true}]}`,
		// windows overlap: store c[i], load c[i+1]
		`{"accesses":[{"id":"s","base":"c","store":true,"size":8,"coef":8,"lo":0,"hi":0},
		              {"id":"l","base":"c","store":false,"size":8,"coef":8,"lo":8,"hi":8}],
		  "pairs":[{"a":"s","b":"l","why":"window"},{"a":"s","b":"s","why":"window"}]}`,
		// a store pair without a reason
		`{"accesses":[{"id":"s","base":"c","store":true,"size":8,"coef":8,"lo":0,"hi":0},
		              {"id":"m","base":"a","store":false,"size":8,"coef":8,"lo":0,"hi":0}],
		  "pairs":[{"a":"s","b":"s","why":"window"}]}`,
		// strided with too many columns for the row stride
		`{"accesses":[{"id":"s","base":"o","store":true,"size":8,"coef":8,"lo":0,"hi":0,"stride":800,"count":100}],
		  "pairs":[{"a":"s","b":"s","why":"strided"}]}`,
		// symbolic start, span too wide
		`{"accesses":[{"id":0,"base":"o","store":true,"size":4,"coef":4,"lo":"sym","hi":"sym","span":4}],
		  "pairs":[{"a":0,"b":0,"why":"window"}]}`,
		// load row[c+1] relative to the store row[c]: dlo = dhi = 4 overlaps the next iteration
		`{"accesses":[{"id":0,"base":"o","store":true,"size":4,"coef":4,"lo":"sym","hi":"sym","span":0},
		              {"id":1,"base":"o","store":false,"size":4,"coef":4,"lo":"sym","hi":"sym","span":0}],
		  "pairs":[{"a":0,"b":0,"why":"window"},{"a":0,"b":1,"why":"window","dlo":4,"dhi":4}]}`,
	}
	for _, s := range reject {
		if bad, _ := CheckLoop(loop(t, s)); len(bad) == 0 {
			t.Errorf("accepted an invalid report: %s", s)
		}
	}
}
