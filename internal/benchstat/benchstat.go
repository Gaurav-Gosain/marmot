// Package benchstat parses Go benchmark output and compares two sets of
// results the way benchstat does: the median of each benchmark's samples,
// the spread around it, the change between the medians, and a two-sided
// Mann-Whitney U test for whether the change is significant.
package benchstat

import (
	"bufio"
	"fmt"
	"io"
	"math"
	"sort"
	"strconv"
	"strings"
)

// Results maps a benchmark name and unit ("ns/op", "B/op", ...) to its
// samples, in the order they were read.
type Results struct {
	Names []string                        // benchmark names, first seen first
	Units []string                        // units, first seen first
	Vals  map[string]map[string][]float64 // name -> unit -> samples
	Err   error                           // reading the input failed
}

// Parse reads benchmark lines ("BenchmarkX-8  1000  123 ns/op  0 B/op").
// Everything else is ignored.
func Parse(r io.Reader) *Results {
	res := &Results{Vals: map[string]map[string][]float64{}}
	seenUnit := map[string]bool{}
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) < 4 || !strings.HasPrefix(f[0], "Benchmark") {
			continue
		}
		if _, err := strconv.Atoi(f[1]); err != nil {
			continue
		}
		name := strings.TrimPrefix(f[0], "Benchmark")
		if res.Vals[name] == nil {
			res.Vals[name] = map[string][]float64{}
			res.Names = append(res.Names, name)
		}
		for i := 2; i+1 < len(f); i += 2 {
			v, err := strconv.ParseFloat(f[i], 64)
			if err != nil {
				break
			}
			unit := f[i+1]
			res.Vals[name][unit] = append(res.Vals[name][unit], v)
			if !seenUnit[unit] {
				seenUnit[unit] = true
				res.Units = append(res.Units, unit)
			}
		}
	}
	res.Err = sc.Err()
	return res
}

// Summary is the center and spread of one benchmark's samples.
type Summary struct {
	N      int     `json:"n"`
	Median float64 `json:"median"`
	Spread float64 `json:"spread"` // largest relative distance of a sample from the median
}

// Summarize computes the median and spread.
func Summarize(xs []float64) Summary {
	if len(xs) == 0 {
		return Summary{}
	}
	s := append([]float64(nil), xs...)
	sort.Float64s(s)
	m := s[len(s)/2]
	if len(s)%2 == 0 {
		m = (s[len(s)/2-1] + s[len(s)/2]) / 2
	}
	spread := 0.0
	if m != 0 {
		spread = math.Max(math.Abs(s[0]-m), math.Abs(s[len(s)-1]-m)) / math.Abs(m)
	}
	return Summary{N: len(s), Median: m, Spread: spread}
}

// Row compares one benchmark and unit between two result sets.
type Row struct {
	Name  string  `json:"name"`
	Unit  string  `json:"unit"`
	Old   Summary `json:"old"`
	New   Summary `json:"new"`
	Delta float64 `json:"delta"` // (new - old) / old; NaN when old is 0
	P     float64 `json:"p"`     // two-sided Mann-Whitney U p-value
}

// Significant reports whether the change is significant at alpha.
func (r Row) Significant(alpha float64) bool { return r.P < alpha && r.Old.N >= 2 && r.New.N >= 2 }

// Compare matches benchmarks by name (ignoring a -GOMAXPROCS suffix
// difference) and unit.
func Compare(old, new *Results) []Row {
	strip := func(n string) string {
		if i := strings.LastIndexByte(n, '-'); i > 0 {
			if _, err := strconv.Atoi(n[i+1:]); err == nil {
				return n[:i]
			}
		}
		return n
	}
	oldBy := map[string]string{}
	for _, n := range old.Names {
		oldBy[strip(n)] = n
	}
	var rows []Row
	for _, unit := range new.Units {
		for _, n := range new.Names {
			on, ok := oldBy[strip(n)]
			if !ok {
				continue
			}
			a, b := old.Vals[on][unit], new.Vals[n][unit]
			if len(a) == 0 || len(b) == 0 {
				continue
			}
			r := Row{Name: strip(n), Unit: unit, Old: Summarize(a), New: Summarize(b), P: MannWhitney(a, b)}
			r.Delta = math.NaN()
			if r.Old.Median != 0 {
				r.Delta = (r.New.Median - r.Old.Median) / r.Old.Median
			}
			rows = append(rows, r)
		}
	}
	return rows
}

// Geomean returns the geometric mean of new/old medians over the rows of
// a unit (rows with a zero are skipped), as a ratio.
func Geomean(rows []Row, unit string) (float64, int) {
	sum, n := 0.0, 0
	for _, r := range rows {
		if r.Unit != unit || r.Old.Median <= 0 || r.New.Median <= 0 {
			continue
		}
		sum += math.Log(r.New.Median / r.Old.Median)
		n++
	}
	if n == 0 {
		return math.NaN(), 0
	}
	return math.Exp(sum / float64(n)), n
}

// MannWhitney returns the two-sided p-value of the Mann-Whitney U test,
// exact for small samples without ties and by the normal approximation
// (with tie and continuity correction) otherwise.
func MannWhitney(a, b []float64) float64 {
	n1, n2 := len(a), len(b)
	if n1 == 0 || n2 == 0 {
		return 1
	}
	type obs struct {
		v float64
		g int
	}
	all := make([]obs, 0, n1+n2)
	for _, x := range a {
		all = append(all, obs{x, 0})
	}
	for _, x := range b {
		all = append(all, obs{x, 1})
	}
	sort.Slice(all, func(i, j int) bool { return all[i].v < all[j].v })
	ranks := make([]float64, len(all))
	ties := false
	tieSum := 0.0
	for i := 0; i < len(all); {
		j := i
		for j < len(all) && all[j].v == all[i].v {
			j++
		}
		r := float64(i+j+1) / 2
		for k := i; k < j; k++ {
			ranks[k] = r
		}
		if t := float64(j - i); t > 1 {
			ties = true
			tieSum += t*t*t - t
		}
		i = j
	}
	r1 := 0.0
	for i, o := range all {
		if o.g == 0 {
			r1 += ranks[i]
		}
	}
	u1 := r1 - float64(n1*(n1+1))/2
	u := math.Min(u1, float64(n1*n2)-u1)
	if !ties && n1 <= 10 && n2 <= 10 {
		return exactP(n1, n2, u)
	}
	n := float64(n1 + n2)
	mu := float64(n1*n2) / 2
	sigma := math.Sqrt(float64(n1*n2) / 12 * ((n + 1) - tieSum/(n*(n-1))))
	if sigma == 0 {
		return 1
	}
	z := (math.Abs(u-mu) - 0.5) / sigma
	return math.Min(1, math.Erfc(z/math.Sqrt2))
}

// exactP counts the arrangements with U at most u (two-sided).
func exactP(n1, n2 int, u float64) float64 {
	// counts[i][j][k]: arrangements of i and j items with U = k.
	maxU := n1 * n2
	memo := map[[3]int]float64{}
	var count func(i, j, k int) float64
	count = func(i, j, k int) float64 {
		if k < 0 {
			return 0
		}
		if i == 0 || j == 0 {
			if k == 0 {
				return 1
			}
			return 0
		}
		key := [3]int{i, j, k}
		if v, ok := memo[key]; ok {
			return v
		}
		v := count(i-1, j, k-j) + count(i, j-1, k)
		memo[key] = v
		return v
	}
	total := 0.0
	for k := 0; k <= maxU; k++ {
		total += count(n1, n2, k)
	}
	le := 0.0
	for k := 0; k <= int(math.Floor(u)); k++ {
		le += count(n1, n2, k)
	}
	return math.Min(1, 2*le/total)
}

// FormatValue prints a value with a unit the way benchstat does (ns/op as
// time, B/op as bytes).
func FormatValue(v float64, unit string) string {
	switch unit {
	case "ns/op":
		switch {
		case v >= 1e9:
			return fmt.Sprintf("%.2fs", v/1e9)
		case v >= 1e6:
			return fmt.Sprintf("%.2fms", v/1e6)
		case v >= 1e3:
			return fmt.Sprintf("%.2fµs", v/1e3)
		}
		return fmt.Sprintf("%.2fns", v)
	case "B/op":
		switch {
		case v >= 1<<20:
			return fmt.Sprintf("%.1fMiB", v/(1<<20))
		case v >= 1<<10:
			return fmt.Sprintf("%.1fKiB", v/(1<<10))
		}
		return fmt.Sprintf("%.0fB", v)
	}
	return strconv.FormatFloat(v, 'g', 4, 64)
}
