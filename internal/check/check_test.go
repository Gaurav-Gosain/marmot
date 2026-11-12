package check

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestParseNolint(t *testing.T) {
	for _, c := range []struct {
		text    string
		linters []string
		ok      bool
	}{
		{"//nolint", nil, true},
		{"// nolint", nil, true},
		{"//nolint:errcheck", []string{"errcheck"}, true},
		{"//nolint:errcheck,gosec // reason", []string{"errcheck", "gosec"}, true},
		{"//nolint:all", nil, true},
		{"//nolintx", nil, false},
		{"// not a directive", nil, false},
	} {
		got, ok := parseNolint(c.text)
		if ok != c.ok || !slices.Equal(got, c.linters) {
			t.Errorf("parseNolint(%q) = %v, %v; want %v, %v", c.text, got, ok, c.linters, c.ok)
		}
	}
}

func TestFilterChecks(t *testing.T) {
	names := []string{"SA1000", "SA4006", "S1000", "ST1000", "ST1003", "QF1001"}
	on := filterChecks(names, []string{"all", "-ST1000", "-QF*", "-SA4*"})
	var got []string
	for _, n := range names {
		if on[n] {
			got = append(got, n)
		}
	}
	if want := []string{"SA1000", "S1000", "ST1003"}; !slices.Equal(got, want) {
		t.Errorf("enabled %v, want %v", got, want)
	}
	// S* is the category S, not SA or ST.
	on = filterChecks(names, []string{"S*"})
	if !on["S1000"] || on["SA1000"] || on["ST1000"] {
		t.Errorf("S* matched %v", on)
	}
}

func TestGoAtLeast(t *testing.T) {
	for _, c := range []struct {
		v, min string
		want   bool
	}{{"1.22", "1.22", true}, {"1.21.5", "1.22", false}, {"1.26", "1.22", true}, {"", "1.22", true}} {
		if got := goAtLeast(c.v, c.min); got != c.want {
			t.Errorf("goAtLeast(%q, %q) = %v", c.v, c.min, got)
		}
	}
}

// findings lists a result as sorted "check analyzer file:line" strings.
func findings(r *Result) []string {
	var out []string
	for _, c := range r.Checks {
		for _, d := range c.Findings {
			out = append(out, fmt.Sprintf("%s %s %s:%d", c.Name, d.Analyzer, filepath.Base(d.Span.File), d.Span.Line))
		}
	}
	return out
}

func TestRunModule(t *testing.T) {
	if testing.Short() {
		t.Skip("loads the standard library")
	}
	t.Setenv("GOWORK", "off")
	t.Setenv("GOFLAGS", "")
	dir, _ := filepath.Abs("testdata/mod")
	cache := t.TempDir()
	opts := Options{Dir: dir, Only: []string{"fmt", "vet", "lint", "modernize"}, CacheDir: cache, Jobs: 2}
	r1, err := Run(opts)
	if err != nil {
		t.Fatal(err)
	}
	got := findings(r1)
	want := []string{
		"fmt gofmt b.go:3",
		"vet printf a.go:11",
		"lint printf a.go:11",
		"lint errcheck a.go:17",
		"lint unused a.go:25",
		"modernize rangeint a.go:30",
	}
	if !slices.Equal(got, want) {
		t.Errorf("findings:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	// errcheck and gosec report on the same line; golangci-lint keeps the
	// first linter by name.
	for _, c := range r1.Checks {
		if c.Name == "lint" && c.Status != Failed {
			t.Errorf("lint status %s, want failed", c.Status)
		}
		if c.Name == "modernize" && c.Status != Findings {
			t.Errorf("modernize status %s, want findings (report only)", c.Status)
		}
	}
	if r1.Stats.Hits != 0 || r1.Stats.Misses == 0 {
		t.Errorf("first run: %d hits, %d misses", r1.Stats.Hits, r1.Stats.Misses)
	}
	// The second run is answered from the cache with the same result.
	r2, err := Run(opts)
	if err != nil {
		t.Fatal(err)
	}
	if r2.Stats.Misses != 0 || r2.Stats.Analyzed != 0 {
		t.Errorf("second run: %d misses, %d analyzed", r2.Stats.Misses, r2.Stats.Analyzed)
	}
	if g2 := findings(r2); !slices.Equal(g2, got) {
		t.Errorf("cached run differs:\n%s", strings.Join(g2, "\n"))
	}
	// The modernizer's fix is machine applicable.
	for _, c := range r1.Checks {
		for _, d := range c.Findings {
			if c.Name == "modernize" && (len(d.Fixes) == 0 || d.Fixes[0].Applicability != "machine-applicable") {
				t.Errorf("modernize finding without a machine applicable fix: %+v", d)
			}
		}
	}
}
