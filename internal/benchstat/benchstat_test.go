package benchstat

import (
	"math"
	"strings"
	"testing"
)

func TestParseAndCompare(t *testing.T) {
	old := Parse(strings.NewReader(`goos: darwin
BenchmarkFib-8   	 1000	      200 ns/op	       0 B/op
BenchmarkFib-8   	 1000	      210 ns/op	       0 B/op
BenchmarkFib-8   	 1000	      205 ns/op	       0 B/op
BenchmarkFib-8   	 1000	      202 ns/op	       0 B/op
BenchmarkFib-8   	 1000	      208 ns/op	       0 B/op
PASS
`))
	new := Parse(strings.NewReader(`BenchmarkFib-10   	 2000	      100 ns/op	       0 B/op
BenchmarkFib-10   	 2000	      104 ns/op	       0 B/op
BenchmarkFib-10   	 2000	      101 ns/op	       0 B/op
BenchmarkFib-10   	 2000	      103 ns/op	       0 B/op
BenchmarkFib-10   	 2000	      102 ns/op	       0 B/op
`))
	rows := Compare(old, new)
	if len(rows) != 2 {
		t.Fatalf("%d rows", len(rows))
	}
	r := rows[0]
	if r.Name != "Fib" || r.Unit != "ns/op" || r.Old.Median != 205 || r.New.Median != 102 {
		t.Errorf("row %+v", r)
	}
	if math.Abs(r.Delta-(102.0-205)/205) > 1e-9 || !r.Significant(0.05) {
		t.Errorf("delta %v p %v", r.Delta, r.P)
	}
	// All five new samples are below all old ones: exact p = 2/252.
	if math.Abs(r.P-2.0/252) > 1e-9 {
		t.Errorf("p = %v, want %v", r.P, 2.0/252)
	}
}

func TestMannWhitneySame(t *testing.T) {
	if p := MannWhitney([]float64{1, 2, 3}, []float64{1, 2, 3}); p < 0.5 {
		t.Errorf("identical samples p = %v", p)
	}
}
