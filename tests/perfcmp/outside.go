package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Outside load: when the load passes its limit or a run shares the LLC, perfcmp logs who
// else used the machine since its last sample: the 5 processes outside its own cgroup with
// the most CPU time, the CPU of kswapd and the pages swapped in and out. A gate that comes
// out INCONCLUSIVE then names the source. Linux only (/proc); elsewhere it logs nothing.

var procRoot = "/proc" // a var for the tests

// procSnap is one sample of /proc: CPU ticks (user plus system) per pid and the swap counters.
type procSnap struct {
	t             time.Time
	ticks         map[int]uint64
	comm          map[int]string
	swpin, swpout uint64
}

// clkTck is USER_HZ, the unit of the tick fields in /proc/PID/stat: 100 on every Linux
// target Go supports.
const clkTck = 100

var outsideLast *procSnap

// outsideMark takes the sample that the next report counts from.
func outsideMark() { outsideLast = takeSnap() }

// outsideReport writes the outside load since the last sample to w, then takes a new one.
func outsideReport(w io.Writer, why string) {
	cur := takeSnap()
	if outsideLast != nil && cur != nil {
		for _, l := range outsideLines(outsideLast, cur, why, ownCgroup()) {
			fmt.Fprintln(w, l)
		}
	}
	outsideLast = cur
}

func takeSnap() *procSnap {
	ents, err := os.ReadDir(procRoot)
	if err != nil {
		return nil
	}
	s := &procSnap{t: time.Now(), ticks: map[int]uint64{}, comm: map[int]string{}}
	for _, e := range ents {
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		comm, t, ok := readStat(pid)
		if ok {
			s.ticks[pid], s.comm[pid] = t, comm
		}
	}
	if data, err := os.ReadFile(filepath.Join(procRoot, "vmstat")); err == nil {
		for l := range strings.SplitSeq(string(data), "\n") {
			f := strings.Fields(l)
			if len(f) != 2 {
				continue
			}
			v, _ := strconv.ParseUint(f[1], 10, 64)
			switch f[0] {
			case "pswpin":
				s.swpin = v
			case "pswpout":
				s.swpout = v
			}
		}
	}
	return s
}

// readStat returns the command name and the user plus system ticks of pid.
func readStat(pid int) (string, uint64, bool) {
	data, err := os.ReadFile(filepath.Join(procRoot, strconv.Itoa(pid), "stat"))
	if err != nil {
		return "", 0, false
	}
	// pid (comm) state ...: comm may hold spaces and parentheses, so split at the last ")".
	open, end := bytes.IndexByte(data, '('), bytes.LastIndexByte(data, ')')
	if open < 0 || end < open {
		return "", 0, false
	}
	f := strings.Fields(string(data[end+1:]))
	if len(f) < 13 { // state is f[0], utime f[11], stime f[12]
		return "", 0, false
	}
	u, err1 := strconv.ParseUint(f[11], 10, 64)
	st, err2 := strconv.ParseUint(f[12], 10, 64)
	if err1 != nil || err2 != nil {
		return "", 0, false
	}
	return string(data[open+1 : end]), u + st, true
}

// cgroupOf is the cgroup v2 path of pid ("" when unknown).
func cgroupOf(pid string) string {
	data, err := os.ReadFile(filepath.Join(procRoot, pid, "cgroup"))
	if err != nil {
		return ""
	}
	for l := range strings.SplitSeq(string(data), "\n") {
		if p, ok := strings.CutPrefix(l, "0::"); ok {
			return p
		}
	}
	return ""
}

func ownCgroup() string { return cgroupOf("self") }

// inside: cg is own or below it (perfcmp, its builds and its timed runs).
func inside(cg, own string) bool {
	return own != "" && own != "/" && (cg == own || strings.HasPrefix(cg, own+"/"))
}

// argsOf is the first 60 characters of pid's command line, or [comm] for a kernel thread.
func argsOf(pid int, comm string) string {
	data, _ := os.ReadFile(filepath.Join(procRoot, strconv.Itoa(pid), "cmdline"))
	a := strings.TrimSpace(strings.ReplaceAll(string(data), "\x00", " "))
	if a == "" {
		return "[" + comm + "]"
	}
	if r := []rune(a); len(r) > 60 {
		a = string(r[:60])
	}
	return a
}

// outsideLines are the log lines for the outside load from prev to cur. A pid that is
// new in cur counts with all its ticks (it started in between, or its pid was reused).
func outsideLines(prev, cur *procSnap, why, own string) []string {
	secs := cur.t.Sub(prev.t).Seconds()
	if secs < 0.5 {
		return nil // too short to say anything
	}
	pct := func(d uint64) float64 { return 100 * float64(d) / clkTck / secs }
	type user struct {
		pid int
		d   uint64
	}
	var users []user
	var kswapd uint64
	for pid, t := range cur.ticks {
		d := t
		if p, ok := prev.ticks[pid]; ok {
			if t < p {
				continue
			}
			d = t - p
		}
		if strings.HasPrefix(cur.comm[pid], "kswapd") {
			kswapd += d
		}
		if d == 0 || inside(cgroupOf(strconv.Itoa(pid)), own) {
			continue
		}
		users = append(users, user{pid, d})
	}
	sort.Slice(users, func(i, j int) bool {
		if users[i].d != users[j].d {
			return users[i].d > users[j].d
		}
		return users[i].pid < users[j].pid
	})
	sub := func(a, b uint64) uint64 {
		if a < b {
			return 0
		}
		return a - b
	}
	lines := []string{fmt.Sprintf("outside load (%s; the last %.0f s): kswapd %.1f%% of one CPU, swap in %d pages, swap out %d pages; the top CPU users outside this job:",
		why, secs, pct(kswapd), sub(cur.swpin, prev.swpin), sub(cur.swpout, prev.swpout))}
	for _, u := range users[:min(5, len(users))] {
		unit := cgroupOf(strconv.Itoa(u.pid))
		if i := strings.LastIndexByte(unit, '/'); i >= 0 {
			unit = unit[i+1:]
		}
		if unit == "" {
			unit = "-"
		}
		lines = append(lines, fmt.Sprintf("  pid %-7d %6.1f%%  %-40s %s", u.pid, pct(u.d), unit, argsOf(u.pid, cur.comm[u.pid])))
	}
	if len(users) == 0 {
		lines = append(lines, "  (none)")
	}
	return lines
}
