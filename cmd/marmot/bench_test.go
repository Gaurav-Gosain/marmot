package main

import (
	"bytes"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/Gaurav-Gosain/marmot/internal/benchstat"
)

// TestCompareWidth renders a comparison at several terminal widths: no
// line is wider than the terminal (a long name may take a line of its
// own), and only the wide layout has p values.
func TestCompareWidth(t *testing.T) {
	rows := []benchstat.Row{
		{Name: "BenchmarkParse-11", Unit: "ns/op", Old: benchstat.Summary{N: 6, Median: 1234.5, Spread: 0.02},
			New: benchstat.Summary{N: 6, Median: 987.6, Spread: 0.01}, Delta: -0.2, P: 0.002},
		{Name: "BenchmarkEncodeVeryLongNameForTheTableWithSubtest/size=1024-11", Unit: "ns/op",
			Old: benchstat.Summary{N: 6, Median: 51234, Spread: 0.03}, New: benchstat.Summary{N: 6, Median: 50000, Spread: 0.04}, Delta: -0.024, P: 0.3},
		{Name: "BenchmarkTiny-11", Unit: "ns/op", Old: benchstat.Summary{N: 6, Median: 2.1, Spread: 0.01},
			New: benchstat.Summary{N: 6, Median: 0.5, Spread: 0.01}, Delta: -0.76, P: 0.002},
	}
	for _, w := range []int{60, 80, 100, 120} {
		var b bytes.Buffer
		printCompareWidth(&b, rows, "go", "marmot", min(w, 100))
		out := b.String()
		for _, l := range strings.Split(out, "\n") {
			if n := utf8.RuneCountInString(l); n > min(w, 100) && !strings.HasPrefix(l, "Benchmark") {
				t.Errorf("width %d: line of %d columns: %q", w, n, l)
			}
		}
		if hasP := strings.Contains(out, "(p="); hasP != (w >= 100) {
			t.Errorf("width %d: p values shown: %v\n%s", w, hasP, out)
		}
		if !strings.Contains(out, "-20.0%") || !strings.Contains(out, "geomean") {
			t.Errorf("width %d: missing the change or geomean\n%s", w, out)
		}
	}
}
