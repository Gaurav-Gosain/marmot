package wakeups

import (
	"fmt"
	"testing"
)

func TestSummarize(t *testing.T) {
	const s = uint64(1e9)
	base := uint64(0x100000000)
	var recs []Record
	// A ticker every second from t=0 to 19 s, made by time.NewTicker
	// (0x1008) called from main.poll (0x2004): the site is main.poll.
	for i := uint64(0); i < 20; i++ {
		recs = append(recs, Record{T: 1000 + i*s, Cause: CauseTimer, Goid: 5, PCs: []uint64{base + 0x1008, base + 0x2004}, Aux: s})
	}
	// A channel readied twice in the window, a GC once, one wakeup in the settle period.
	recs = append(recs,
		Record{T: 1000 + 12*s, Cause: CauseReadied, Goid: 7, PCs: []uint64{base + 0x3010}},
		Record{T: 1000 + 13*s, Cause: CauseReadied, Goid: 7, PCs: []uint64{base + 0x3010}},
		Record{T: 1000 + 14*s, Cause: CauseGC},
		Record{T: 1000 + 2*s, Cause: CauseSignal, Aux: 2},
	)
	data := Encode(Header{Version: 1, PID: 42, Base: base, Start: 1000}, recs)
	h, got, err := Parse(append(data, 1, 2, 3)) // a torn last record is dropped
	if err != nil || len(got) != len(recs) || h.PID != 42 {
		t.Fatalf("parse: %v, %d records, header %+v", err, len(got), h)
	}
	resolve := func(off uint64) Site {
		switch off {
		case 0x2003:
			return Site{"main.poll", "main.go", 12}
		case 0x1007:
			return Site{"time.NewTicker", "sleep.go", 5}
		case 0x300f:
			return Site{"main.produce", "main.go", 30}
		}
		return Site{Function: fmt.Sprintf("?%#x", off)}
	}
	r := Summarize(h, got, 10*s, 10*s, resolve)
	if r.Total != 13 || r.PerSecond != 1.3 {
		t.Errorf("total %d, %.2f/s; want 13, 1.30", r.Total, r.PerSecond)
	}
	if got := fmt.Sprint(r.Causes); got != "[{timer 10 1} {goroutine readied 2 0.2} {GC 1 0.1}]" {
		t.Errorf("causes %s", got)
	}
	top := r.Sites[0]
	if top.Cause != "timer" || top.String() != "main.poll (main.go:12)" || top.Count != 10 || top.PeriodNs != s {
		t.Errorf("top site %+v", top)
	}
	if r.Sites[1].Site.String() != "main.produce (main.go:30)" || r.Sites[1].Count != 2 {
		t.Errorf("second site %+v", r.Sites[1])
	}
	if len(r.PerSecondT) != 10 || r.PerSecondT[2] != 2 || r.PerSecondT[4] != 2 {
		t.Errorf("timeline %v", r.PerSecondT)
	}
}

func TestParseErrors(t *testing.T) {
	if _, _, err := Parse([]byte("nope")); err == nil {
		t.Error("no header: no error")
	}
	if _, _, err := Parse(Encode(Header{Version: 3}, nil)); err == nil {
		t.Error("version 3: no error")
	}
}

// Version 2 carries its frame count: a user frame below three runtime ones
// is found.
func TestVersion2(t *testing.T) {
	base := uint64(0x1000)
	recs := []Record{{T: 5, Cause: CauseTimer, PCs: []uint64{base + 0x11, base + 0x21, base + 0x31, base + 0x41}}}
	h, got, err := Parse(Encode(Header{Version: 2, Base: base, Frames: 8}, recs))
	if err != nil || h.Frames != 8 || len(got) != 1 || got[0].PCs[3] != base+0x41 {
		t.Fatalf("parse: %v %+v %+v", err, h, got)
	}
	names := map[uint64]string{0x10: "runtime.(*timeTimer).start", 0x20: "time.newTimer", 0x30: "time.NewTicker", 0x40: "main.poll"}
	r := Summarize(h, got, 0, 0, func(off uint64) Site { return Site{Function: names[off]} })
	if len(r.Sites) != 1 || r.Sites[0].Function != "main.poll" {
		t.Errorf("sites %+v", r.Sites)
	}
}
