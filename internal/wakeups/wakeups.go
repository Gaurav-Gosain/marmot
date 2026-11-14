// Package wakeups reads the runtime's wakeup trace (MARMOT_TRACE=wakeups=PATH)
// and summarizes what woke a program while it was idle: per cause, and per
// source site (a timer's creation site, the goroutine that readied a
// channel receiver), as wakeups per second.
//
// Trace format, version 1, little endian: the header "MRMTWAKE", u32
// version, u32 pid, u64 image base, u64 runtime start (ns, the records'
// clock); then 48-byte records, one per OS level
// wakeup (a thread returning from a blocking sleep): u64 t_ns (monotonic),
// u8 cause, 3 bytes padding, u32 goroutine id, u64 pc[3], u64 aux. The pcs
// are the three frames above the runtime's hook when the timer was made or
// the goroutine was readied (0 where the frame chain ends); the site is the
// first of them outside the runtime, time and sync packages.
package wakeups

import (
	"encoding/binary"
	"errors"
	"fmt"
	"sort"
	"strings"
)

// Causes, as the runtime numbers them.
const (
	CauseTimer     = 1
	CauseReadied   = 2
	CauseNetpoll   = 3
	CauseGC        = 4
	CauseSignal    = 5
	CauseScheduler = 6
	CauseOther     = 7
)

// CauseName names a cause.
func CauseName(c uint8) string {
	switch c {
	case CauseTimer:
		return "timer"
	case CauseReadied:
		return "goroutine readied"
	case CauseNetpoll:
		return "netpoll"
	case CauseGC:
		return "GC"
	case CauseSignal:
		return "signal"
	case CauseScheduler:
		return "scheduler"
	}
	return "other"
}

const (
	magic      = "MRMTWAKE"
	headerSize = 8 + 4 + 4 + 8 + 8
	recordSize = 48
)

// Header is the trace's header.
type Header struct {
	Version uint32
	PID     uint32
	Base    uint64 // the image base at run time
	Start   uint64 // ns when the runtime started, on the records' clock
}

// Record is one wakeup.
type Record struct {
	T     uint64 // ns, monotonic
	Cause uint8
	Goid  uint32
	PCs   [3]uint64
	Aux   uint64
}

// Parse reads a trace. A trailing partial record (the program was killed
// while writing) is dropped.
func Parse(data []byte) (Header, []Record, error) {
	var h Header
	if len(data) < headerSize || string(data[:8]) != magic {
		return h, nil, errors.New("not a wakeup trace (no MRMTWAKE header)")
	}
	h.Version = binary.LittleEndian.Uint32(data[8:])
	h.PID = binary.LittleEndian.Uint32(data[12:])
	h.Base = binary.LittleEndian.Uint64(data[16:])
	h.Start = binary.LittleEndian.Uint64(data[24:])
	if h.Version != 1 {
		return h, nil, fmt.Errorf("wakeup trace version %d (this marmot reads 1)", h.Version)
	}
	body := data[headerSize:]
	recs := make([]Record, 0, len(body)/recordSize)
	for len(body) >= recordSize {
		recs = append(recs, Record{
			T:     binary.LittleEndian.Uint64(body),
			Cause: body[8],
			Goid:  binary.LittleEndian.Uint32(body[12:]),
			PCs:   [3]uint64{binary.LittleEndian.Uint64(body[16:]), binary.LittleEndian.Uint64(body[24:]), binary.LittleEndian.Uint64(body[32:])},
			Aux:   binary.LittleEndian.Uint64(body[40:]),
		})
		body = body[recordSize:]
	}
	return h, recs, nil
}

// Site is a source location: a function and its file and line.
type Site struct {
	Function string `json:"function,omitempty"`
	File     string `json:"file,omitempty"`
	Line     int    `json:"line,omitempty"`
}

func (s Site) String() string {
	switch {
	case s.Function == "":
		return ""
	case s.File == "":
		return s.Function
	}
	return fmt.Sprintf("%s (%s:%d)", s.Function, s.File, s.Line)
}

// Resolver maps a pc offset from the image base to its site.
type Resolver func(off uint64) Site

// CauseCount is one cause's wakeups.
type CauseCount struct {
	Cause     string  `json:"cause"`
	Count     int     `json:"count"`
	PerSecond float64 `json:"perSecond"`
}

// SiteCount is one site's wakeups of one cause.
type SiteCount struct {
	Cause string `json:"cause"`
	Site
	Count     int     `json:"count"`
	PerSecond float64 `json:"perSecond"`
	// PeriodNs is a ticker's period (the runtime's aux for timers).
	PeriodNs uint64 `json:"periodNs,omitempty"`
}

// Report is the summary of a window of a trace.
type Report struct {
	Kind       string       `json:"kind"` // "wakeups"
	PID        uint32       `json:"pid"`
	SettleSec  float64      `json:"settleSeconds"`
	WindowSec  float64      `json:"windowSeconds"`
	Total      int          `json:"total"`
	PerSecond  float64      `json:"perSecond"`
	Causes     []CauseCount `json:"causes"`
	Sites      []SiteCount  `json:"sites"`
	PerSecondT []int        `json:"timeline"` // wakeups in each second of the window
}

// Summarize counts the wakeups from start+settle to start+settle+window
// (ns; start is the runtime's start from the header). A window of 0 runs to
// the last record.
func Summarize(h Header, recs []Record, settleNs, windowNs uint64, resolve Resolver) *Report {
	r := &Report{Kind: "wakeups", PID: h.PID, Causes: []CauseCount{}, Sites: []SiteCount{}, PerSecondT: []int{}}
	if len(recs) == 0 {
		r.SettleSec, r.WindowSec = float64(settleNs)/1e9, float64(windowNs)/1e9
		return r
	}
	startNs := h.Start
	if startNs == 0 {
		startNs = recs[0].T
	}
	from := startNs + settleNs
	to := ^uint64(0)
	if windowNs > 0 {
		to = from + windowNs
	}
	last := from
	byCause := map[uint8]int{}
	type key struct {
		cause uint8
		site  Site
	}
	sites := map[[3]uint64]Site{}
	siteOf := func(pcs [3]uint64) Site {
		if s, ok := sites[pcs]; ok {
			return s
		}
		s := pickSite(h.Base, pcs, resolve)
		sites[pcs] = s
		return s
	}
	bySite := map[key]int{}
	period := map[key]uint64{}
	for _, x := range recs {
		if x.T < from || x.T >= to {
			continue
		}
		last = max(last, x.T)
		r.Total++
		byCause[x.Cause]++
		k := key{x.Cause, siteOf(x.PCs)}
		bySite[k]++
		if x.Cause == CauseTimer && x.Aux > 0 {
			period[k] = x.Aux
		}
		sec := int((x.T - from) / 1e9)
		for len(r.PerSecondT) <= sec {
			r.PerSecondT = append(r.PerSecondT, 0)
		}
		r.PerSecondT[sec]++
	}
	span := float64(windowNs) / 1e9
	if windowNs == 0 {
		span = float64(last-from) / 1e9
	}
	r.SettleSec, r.WindowSec = float64(settleNs)/1e9, span
	rate := func(n int) float64 {
		if span <= 0 {
			return 0
		}
		return float64(n) / span
	}
	r.PerSecond = rate(r.Total)
	for c, n := range byCause {
		r.Causes = append(r.Causes, CauseCount{CauseName(c), n, rate(n)})
	}
	sort.Slice(r.Causes, func(i, j int) bool {
		if r.Causes[i].Count != r.Causes[j].Count {
			return r.Causes[i].Count > r.Causes[j].Count
		}
		return r.Causes[i].Cause < r.Causes[j].Cause
	})
	for k, n := range bySite {
		r.Sites = append(r.Sites, SiteCount{Cause: CauseName(k.cause), Site: k.site, Count: n, PerSecond: rate(n), PeriodNs: period[k]})
	}
	sort.Slice(r.Sites, func(i, j int) bool {
		a, b := r.Sites[i], r.Sites[j]
		if a.Count != b.Count {
			return a.Count > b.Count
		}
		if a.Cause != b.Cause {
			return a.Cause < b.Cause
		}
		return a.Site.String() < b.Site.String()
	})
	return r
}

// pickSite is the first frame outside the runtime, time and sync packages
// (the runtime cannot tell them apart cheaply when it records), or the
// first frame that resolves when all are inside them.
func pickSite(base uint64, pcs [3]uint64, resolve Resolver) Site {
	if resolve == nil {
		return Site{}
	}
	var first Site
	for _, pc := range pcs {
		if pc <= base {
			continue
		}
		// A frame's pc is a return address: the call is the byte before.
		s := resolve(pc - base - 1)
		if s.Function == "" {
			continue
		}
		if first.Function == "" {
			first = s
		}
		if !internalFunc(s.Function) {
			return s
		}
	}
	return first
}

// internalFunc reports whether fn belongs to the runtime, time or sync
// (or their internal packages), whose frames sit between a wakeup and the
// program's own code.
func internalFunc(fn string) bool {
	fn = strings.TrimLeft(fn, "(*")
	for _, p := range []string{"runtime.", "runtime/", "time.", "sync.", "sync/", "internal/", "context."} {
		if strings.HasPrefix(fn, p) {
			return true
		}
	}
	return false
}

// Encode writes a trace: for tests and tools that make one.
func Encode(h Header, recs []Record) []byte {
	b := make([]byte, headerSize, headerSize+len(recs)*recordSize)
	copy(b, magic)
	binary.LittleEndian.PutUint32(b[8:], h.Version)
	binary.LittleEndian.PutUint32(b[12:], h.PID)
	binary.LittleEndian.PutUint64(b[16:], h.Base)
	binary.LittleEndian.PutUint64(b[24:], h.Start)
	for _, x := range recs {
		var r [recordSize]byte
		binary.LittleEndian.PutUint64(r[0:], x.T)
		r[8] = x.Cause
		binary.LittleEndian.PutUint32(r[12:], x.Goid)
		for i, pc := range x.PCs {
			binary.LittleEndian.PutUint64(r[16+8*i:], pc)
		}
		binary.LittleEndian.PutUint64(r[40:], x.Aux)
		b = append(b, r[:]...)
	}
	return b
}
