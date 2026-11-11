package specialize

import "testing"

// sortProgram sorts many inputs through sort.Slice, sort.SliceStable,
// sort.Sort and sort.Stable. Equal keys, inconsistent comparators (driven by
// a random generator, so any change in the sequence of Less calls shows) and
// input patterns that reach every pdqsort path make the output depend on the
// exact algorithm, not just on sortedness.
const sortProgram = `package main

import (
	"fmt"
	"os"
	"sort"
	"strings"
)

type rec struct {
	key  int
	name string
}

type byKey []rec

func (s byKey) Len() int           { return len(s) }
func (s byKey) Less(i, j int) bool { return s[i].key < s[j].key }
func (s byKey) Swap(i, j int)      { s[i], s[j] = s[j], s[i] }

type ptrSorter struct {
	v     []int
	calls int
}

func (p *ptrSorter) Len() int           { return len(p.v) }
func (p *ptrSorter) Less(i, j int) bool { p.calls++; return p.v[i]%7 < p.v[j]%7 }
func (p *ptrSorter) Swap(i, j int)      { p.v[i], p.v[j] = p.v[j], p.v[i] }

var out strings.Builder

type rng uint64

func (r *rng) next() uint64 {
	*r ^= *r << 13
	*r ^= *r >> 7
	*r ^= *r << 17
	return uint64(*r)
}

func inputs() [][]rec {
	var all [][]rec
	r := rng(12345)
	for _, n := range []int{0, 1, 2, 3, 7, 12, 13, 50, 100, 1000, 5000} {
		random := make([]rec, n)
		few := make([]rec, n)
		sorted := make([]rec, n)
		rev := make([]rec, n)
		saw := make([]rec, n)
		for i := 0; i < n; i++ {
			random[i] = rec{int(r.next() % 1000), fmt.Sprint("r", i)}
			few[i] = rec{int(r.next() % 3), fmt.Sprint("f", i)}
			sorted[i] = rec{i, fmt.Sprint("s", i)}
			rev[i] = rec{n - i, fmt.Sprint("v", i)}
			saw[i] = rec{i % 17, fmt.Sprint("w", i)}
		}
		all = append(all, random, few, sorted, rev, saw)
	}
	return all
}

func dump(tag string, s []rec) {
	fmt.Fprintf(&out, "%s %d:", tag, len(s))
	for i, x := range s {
		if i < 40 || i%97 == 0 {
			fmt.Fprintf(&out, " %d/%s", x.key, x.name)
		}
	}
	out.WriteByte('\n')
}

func main() {
	for i, in := range inputs() {
		a := append([]rec(nil), in...)
		sort.Slice(a, func(i, j int) bool { return a[i].key < a[j].key })
		dump(fmt.Sprint("slice", i), a)

		b := append([]rec(nil), in...)
		calls := 0
		sort.Slice(b, func(x, y int) bool {
			calls++
			if b[x].key != b[y].key {
				return b[x].key > b[y].key
			}
			return b[x].name < b[y].name
		})
		dump(fmt.Sprint("slice2-", i, "-", calls), b)

		c := append([]rec(nil), in...)
		sort.SliceStable(c, func(i, j int) bool { return c[i].key/10 < c[j].key/10 })
		dump(fmt.Sprint("stable", i), c)

		d := append(byKey(nil), in...)
		sort.Sort(d)
		dump(fmt.Sprint("sort", i), d)

		e := append(byKey(nil), in...)
		sort.Stable(e)
		dump(fmt.Sprint("sstable", i), e)

		// Inconsistent comparator: the result depends on every call.
		f := append([]rec(nil), in...)
		r := rng(uint64(i) + 99)
		sort.Slice(f, func(i, j int) bool { return r.next()%3 == 0 })
		dump(fmt.Sprint("chaos", i), f)

		// A less that is not a literal.
		g := append([]rec(nil), in...)
		less := func(i, j int) bool { return g[i].name < g[j].name }
		sort.Slice(g, less)
		dump(fmt.Sprint("named", i), g)

		// Local type: the comparator stays a func value.
		type kv struct {
			k string
			v int
		}
		h := make([]kv, len(in))
		for j, x := range in {
			h[j] = kv{x.name, x.key % 5}
		}
		sort.Slice(h, func(i, j int) bool { return h[i].v < h[j].v })
		fmt.Fprintln(&out, "local", i, len(h), h[:min(len(h), 20)])

		p := &ptrSorter{v: make([]int, len(in))}
		for j, x := range in {
			p.v[j] = x.key
		}
		sort.Sort(p)
		fmt.Fprintln(&out, "ptr", i, p.calls, p.v[:min(len(p.v), 30)])
	}
	strs := []string{"b", "a", "c", "a"}
	sort.Slice(strs, func(i, j int) bool { return strings.Compare(strs[i], strs[j]) < 0 })
	fmt.Fprintln(&out, strs)
	defer func() { fmt.Fprintln(&out, "recovered:", recover() != nil); os.Stdout.WriteString(out.String()) }()
	var nilLess func(i, j int) bool
	sort.Slice([]int{2, 1}, nilLess)
}
`

func TestSortDifferential(t *testing.T) {
	if testing.Short() {
		t.Skip("builds programs with the go tool")
	}
	diffTest(t, sortProgram, 8)
}
