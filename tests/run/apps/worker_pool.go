// A staged worker pool pipeline: generate, parse, compute, aggregate. Results are ordered
// deterministically by sequence number even though workers run concurrently.
package main

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
)

type Job struct {
	Seq  int
	Line string
}

type Parsed struct {
	Seq    int
	Name   string
	Values []int
	Err    error
}

type Result struct {
	Seq   int
	Name  string
	Sum   int
	Max   int
	Hash  uint32
	Err   error
	Stage string
}

func generate(n int) <-chan Job {
	out := make(chan Job)
	go func() {
		defer close(out)
		for i := 0; i < n; i++ {
			var sb strings.Builder
			fmt.Fprintf(&sb, "item%03d:", i)
			cnt := i%7 + 1
			for j := 0; j < cnt; j++ {
				if j > 0 {
					sb.WriteByte(',')
				}
				if i%23 == 22 && j == 1 {
					sb.WriteString("bad")
				} else {
					sb.WriteString(strconv.Itoa((i*31 + j*17) % 100))
				}
			}
			out <- Job{i, sb.String()}
		}
	}()
	return out
}

func parseStage(in <-chan Job, workers int) <-chan Parsed {
	out := make(chan Parsed, 4)
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range in {
				p := Parsed{Seq: j.Seq}
				name, rest, ok := strings.Cut(j.Line, ":")
				if !ok {
					p.Err = fmt.Errorf("no colon")
				} else {
					p.Name = name
					for _, f := range strings.Split(rest, ",") {
						v, err := strconv.Atoi(f)
						if err != nil {
							p.Err = fmt.Errorf("%s: %w", name, err)
							break
						}
						p.Values = append(p.Values, v)
					}
				}
				out <- p
			}
		}()
	}
	go func() {
		wg.Wait()
		close(out)
	}()
	return out
}

func fnv(s string) uint32 {
	h := uint32(2166136261)
	for i := 0; i < len(s); i++ {
		h ^= uint32(s[i])
		h *= 16777619
	}
	return h
}

func computeStage(in <-chan Parsed, workers int) <-chan Result {
	out := make(chan Result, 4)
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for p := range in {
				r := Result{Seq: p.Seq, Name: p.Name, Err: p.Err, Stage: "compute"}
				if p.Err == nil {
					for _, v := range p.Values {
						r.Sum += v
						r.Max = max(r.Max, v)
					}
					r.Hash = fnv(fmt.Sprint(p.Name, p.Values))
				}
				out <- r
			}
		}()
	}
	go func() {
		wg.Wait()
		close(out)
	}()
	return out
}

type Stats struct {
	mu     sync.Mutex
	count  int
	errors int
	total  int
	byMax  map[int]int
}

func (s *Stats) add(r Result) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.count++
	if r.Err != nil {
		s.errors++
		return
	}
	s.total += r.Sum
	s.byMax[r.Max/25*25]++
}

func run(n, w int) ([]Result, *Stats) {
	st := &Stats{byMax: map[int]int{}}
	var results []Result
	var mu sync.Mutex
	var wg sync.WaitGroup
	res := computeStage(parseStage(generate(n), w), w)
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for r := range res {
				st.add(r)
				mu.Lock()
				results = append(results, r)
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	sort.Slice(results, func(i, j int) bool { return results[i].Seq < results[j].Seq })
	return results, st
}

func main() {
	var baseline []Result
	for _, w := range []int{1, 2, 4, 8} {
		results, st := run(500, w)
		fmt.Printf("workers=%d count=%d errors=%d total=%d\n", w, st.count, st.errors, st.total)
		if baseline == nil {
			baseline = results
			continue
		}
		same := len(results) == len(baseline)
		for i := range results {
			if same && (results[i].Hash != baseline[i].Hash || results[i].Sum != baseline[i].Sum || (results[i].Err == nil) != (baseline[i].Err == nil)) {
				same = false
			}
		}
		fmt.Println("matches baseline:", same)
	}
	for _, r := range baseline[:6] {
		fmt.Printf("%3d %s sum=%d max=%d hash=%08x err=%v\n", r.Seq, r.Name, r.Sum, r.Max, r.Hash, r.Err)
	}
	for _, r := range baseline {
		if r.Err != nil {
			fmt.Println("error:", r.Err)
			break
		}
	}
	_, st := run(500, 3)
	keys := make([]int, 0)
	for k := range st.byMax {
		keys = append(keys, k)
	}
	sort.Ints(keys)
	for _, k := range keys {
		fmt.Printf("max in [%d,%d): %d\n", k, k+25, st.byMax[k])
	}

	// A bounded semaphore limiting concurrency.
	sem := make(chan struct{}, 3)
	var active, peak int
	var amu sync.Mutex
	var wg sync.WaitGroup
	for i := 0; i < 30; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			amu.Lock()
			active++
			peak = max(peak, active)
			amu.Unlock()
			for k := 0; k < 1000; k++ {
				_ = k * k
			}
			amu.Lock()
			active--
			amu.Unlock()
			<-sem
		}()
	}
	wg.Wait()
	fmt.Println("peak concurrency <= 3:", peak <= 3, "active:", active)
}
