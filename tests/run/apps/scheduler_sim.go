// A discrete-event CPU scheduler simulation comparing FCFS, SJF, round robin and a
// multilevel priority scheduler with aging, built on container/heap.
package main

import (
	"container/heap"
	"fmt"
	"sort"
	"strings"
)

type Proc struct {
	ID       int
	Name     string
	Arrive   int
	Burst    int
	Priority int
	remain   int
	start    int
	finish   int
	started  bool
	waitFrom int
	queued   int
}

type Event struct {
	Time int
	ID   int
	What string
}

type Result struct {
	Algo     string
	Procs    []*Proc
	Timeline []Event
	Gantt    string
	Switches int
}

func workload() []*Proc {
	specs := []struct {
		name             string
		arrive, burst, p int
	}{
		{"init", 0, 8, 3}, {"shell", 1, 4, 1}, {"cron", 2, 9, 4}, {"editor", 3, 5, 2},
		{"build", 4, 12, 3}, {"mail", 6, 3, 1}, {"backup", 8, 15, 5}, {"web", 9, 6, 2},
		{"log", 20, 2, 1}, {"db", 21, 10, 2},
	}
	var ps []*Proc
	for i, s := range specs {
		ps = append(ps, &Proc{ID: i + 1, Name: s.name, Arrive: s.arrive, Burst: s.burst, Priority: s.p, remain: s.burst})
	}
	return ps
}

func clone(ps []*Proc) []*Proc {
	out := make([]*Proc, len(ps))
	for i, p := range ps {
		c := *p
		c.remain, c.started, c.start, c.finish = c.Burst, false, 0, 0
		out[i] = &c
	}
	return out
}

type pq struct {
	items []*Proc
	less  func(a, b *Proc) bool
}

func (q *pq) Len() int           { return len(q.items) }
func (q *pq) Less(i, j int) bool { return q.less(q.items[i], q.items[j]) }
func (q *pq) Swap(i, j int)      { q.items[i], q.items[j] = q.items[j], q.items[i] }
func (q *pq) Push(x any)         { q.items = append(q.items, x.(*Proc)) }
func (q *pq) Pop() any {
	n := len(q.items)
	x := q.items[n-1]
	q.items = q.items[:n-1]
	return x
}

type sim struct {
	now      int
	gantt    strings.Builder
	timeline []Event
	switches int
	last     int
}

func (s *sim) run(p *Proc, d int) {
	if s.last != p.ID {
		s.switches++
		s.last = p.ID
	}
	if !p.started {
		p.started = true
		p.start = s.now
		s.timeline = append(s.timeline, Event{s.now, p.ID, "start"})
	}
	s.gantt.WriteString(strings.Repeat(string(rune('A'+p.ID-1)), d))
	s.now += d
	p.remain -= d
	if p.remain == 0 {
		p.finish = s.now
		s.timeline = append(s.timeline, Event{s.now, p.ID, "done"})
	}
}

func (s *sim) idle(until int) {
	s.gantt.WriteString(strings.Repeat(".", until-s.now))
	s.now = until
	s.last = 0
}

// nonPreemptive runs FCFS or SJF depending on the comparison.
func nonPreemptive(name string, ps []*Proc, less func(a, b *Proc) bool) Result {
	ps = clone(ps)
	sort.Slice(ps, func(i, j int) bool { return ps[i].Arrive < ps[j].Arrive })
	s := &sim{}
	q := &pq{less: less}
	next := 0
	done := 0
	for done < len(ps) {
		for next < len(ps) && ps[next].Arrive <= s.now {
			heap.Push(q, ps[next])
			next++
		}
		if q.Len() == 0 {
			s.idle(ps[next].Arrive)
			continue
		}
		p := heap.Pop(q).(*Proc)
		s.run(p, p.remain)
		done++
	}
	return Result{name, ps, s.timeline, s.gantt.String(), s.switches}
}

func roundRobin(ps []*Proc, quantum int) Result {
	ps = clone(ps)
	sort.Slice(ps, func(i, j int) bool { return ps[i].Arrive < ps[j].Arrive })
	s := &sim{}
	var queue []*Proc
	next, done := 0, 0
	admit := func() {
		for next < len(ps) && ps[next].Arrive <= s.now {
			queue = append(queue, ps[next])
			next++
		}
	}
	for done < len(ps) {
		admit()
		if len(queue) == 0 {
			s.idle(ps[next].Arrive)
			continue
		}
		p := queue[0]
		queue = queue[1:]
		d := min(quantum, p.remain)
		s.run(p, d)
		admit()
		if p.remain > 0 {
			queue = append(queue, p)
		} else {
			done++
		}
	}
	return Result{fmt.Sprintf("RR(q=%d)", quantum), ps, s.timeline, s.gantt.String(), s.switches}
}

// priorityAging: preemptive priority (lower number runs first) where every 10 ticks of
// waiting improves the effective priority by one level.
func priorityAging(ps []*Proc) Result {
	ps = clone(ps)
	sort.Slice(ps, func(i, j int) bool { return ps[i].Arrive < ps[j].Arrive })
	s := &sim{}
	var ready []*Proc
	next, done := 0, 0
	eff := func(p *Proc) int { return p.Priority - (s.now-p.waitFrom)/10 }
	for done < len(ps) {
		for next < len(ps) && ps[next].Arrive <= s.now {
			ps[next].waitFrom = ps[next].Arrive
			ready = append(ready, ps[next])
			next++
		}
		if len(ready) == 0 {
			s.idle(ps[next].Arrive)
			continue
		}
		sort.SliceStable(ready, func(i, j int) bool {
			ei, ej := eff(ready[i]), eff(ready[j])
			if ei != ej {
				return ei < ej
			}
			return ready[i].Arrive < ready[j].Arrive
		})
		p := ready[0]
		d := 1
		s.run(p, d)
		p.waitFrom = s.now
		for _, o := range ready[1:] {
			_ = o
		}
		if p.remain == 0 {
			ready = ready[1:]
			done++
		}
	}
	return Result{"priority+aging", ps, s.timeline, s.gantt.String(), s.switches}
}

func report(r Result) {
	fmt.Printf("== %s: switches=%d\n%s\n", r.Algo, r.Switches, r.Gantt)
	ps := append([]*Proc(nil), r.Procs...)
	sort.Slice(ps, func(i, j int) bool { return ps[i].ID < ps[j].ID })
	totalWait, totalTurn, totalResp := 0, 0, 0
	fmt.Println("id name     arrive burst  start finish  wait  turn")
	for _, p := range ps {
		turn := p.finish - p.Arrive
		wait := turn - p.Burst
		totalWait += wait
		totalTurn += turn
		totalResp += p.start - p.Arrive
		fmt.Printf("%2d %-8s %5d %5d %6d %6d %5d %5d\n", p.ID, p.Name, p.Arrive, p.Burst, p.start, p.finish, wait, turn)
	}
	n := float64(len(ps))
	fmt.Printf("avg wait %.2f  avg turnaround %.2f  avg response %.2f  makespan %d\n", float64(totalWait)/n, float64(totalTurn)/n, float64(totalResp)/n, ps[0].finish)
	mk := 0
	for _, p := range ps {
		mk = max(mk, p.finish)
	}
	fmt.Println("check:", validate(r, mk))
}

func validate(r Result, makespan int) string {
	work := 0
	for _, c := range r.Gantt {
		if c != '.' {
			work++
		}
	}
	total := 0
	for _, p := range r.Procs {
		total += p.Burst
		if p.finish < p.Arrive+p.Burst {
			return fmt.Sprintf("proc %d finished too early", p.ID)
		}
	}
	if work != total {
		return fmt.Sprintf("work %d != total burst %d", work, total)
	}
	if len(r.Gantt) != makespan {
		return fmt.Sprintf("gantt length %d != makespan %d", len(r.Gantt), makespan)
	}
	return "ok"
}

func main() {
	ps := workload()
	fcfs := nonPreemptive("FCFS", ps, func(a, b *Proc) bool { return a.Arrive < b.Arrive || a.Arrive == b.Arrive && a.ID < b.ID })
	sjf := nonPreemptive("SJF", ps, func(a, b *Proc) bool {
		if a.Burst != b.Burst {
			return a.Burst < b.Burst
		}
		return a.ID < b.ID
	})
	results := []Result{fcfs, sjf, roundRobin(ps, 2), roundRobin(ps, 5), priorityAging(ps)}
	for _, r := range results {
		report(r)
	}
	best, bestWait := "", 1e9
	for _, r := range results {
		w := 0
		for _, p := range r.Procs {
			w += p.finish - p.Arrive - p.Burst
		}
		avg := float64(w) / float64(len(r.Procs))
		if avg < bestWait {
			best, bestWait = r.Algo, avg
		}
	}
	fmt.Printf("best average wait: %s (%.2f)\n", best, bestWait)
	for _, e := range results[4].Timeline[:6] {
		fmt.Printf("t=%d p%d %s\n", e.Time, e.ID, e.What)
	}
}
