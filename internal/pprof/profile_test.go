package pprof

import (
	"bytes"
	rpprof "runtime/pprof"
	"strings"
	"testing"
	"time"
)

var sink int

func spin(d time.Duration) {
	end := time.Now().Add(d)
	for time.Now().Before(end) {
		for i := range 1000 {
			sink += i * i
		}
	}
}

// A CPU profile written by runtime/pprof decodes, and the spinning
// function shows up with most of the samples.
func TestCPUProfile(t *testing.T) {
	var buf bytes.Buffer
	if err := rpprof.StartCPUProfile(&buf); err != nil {
		t.Skip(err)
	}
	spin(300 * time.Millisecond)
	rpprof.StopCPUProfile()
	p, err := Parse(buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	idx, err := p.Index("cpu")
	if err != nil {
		t.Fatal(err)
	}
	funcs, total := p.ByFunction(idx)
	if total == 0 || len(p.Samples) == 0 {
		t.Skip("no samples on this machine")
	}
	found := false
	for _, f := range funcs {
		if strings.HasSuffix(f.Name, ".spin") && f.Cum*2 > total {
			found = true
		}
	}
	if !found {
		t.Errorf("spin is not the hot function: %+v", funcs[:min(5, len(funcs))])
	}
	if tr := p.Tree(idx); tr.Value != total {
		t.Errorf("tree total %d, want %d", tr.Value, total)
	}
}

func TestHeapProfile(t *testing.T) {
	var buf bytes.Buffer
	if err := rpprof.WriteHeapProfile(&buf); err != nil {
		t.Fatal(err)
	}
	p, err := Parse(buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.Index("alloc_space"); err != nil {
		t.Error(err)
	}
}
