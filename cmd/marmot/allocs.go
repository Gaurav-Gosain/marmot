package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/Gaurav-Gosain/marmot/internal/diag"
)

// allocRec is one line of the code generator's allocation report
// (MARMOT_ALLOC_REPORT, internal/codegen/allocreport.go).
type allocRec struct {
	Fn     string `json:"fn"`
	Pos    string `json:"pos"`
	Kind   string `json:"kind"`
	Type   string `json:"type,omitempty"`
	Bytes  int64  `json:"bytes,omitempty"`
	Where  string `json:"where"`
	Reason string `json:"reason,omitempty"`
	Loop   bool   `json:"loop,omitempty"`
	Final  bool   `json:"final"`
}

// allocReportFile makes the file a build with -allocs writes its report to.
func allocReportFile() (*os.File, func(), error) {
	f, err := os.CreateTemp("", "marmot-allocs-*.jsonl")
	if err != nil {
		return nil, nil, err
	}
	return f, func() { f.Close(); os.Remove(f.Name()) }, nil
}

// showAllocs prints the allocation sites of the main module (or of every
// package with all): heap and region allocations as notes at their source
// line with the reason, then a summary.
func showAllocs(rep *diag.Reporter, path string, all bool) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()
	root, _ := findModuleRoot(rep.Dir)
	seen := map[string]bool{}
	var recs []allocRec
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<24)
	for sc.Scan() {
		var r allocRec
		if json.Unmarshal(sc.Bytes(), &r) != nil || r.Pos == "" {
			continue
		}
		if !all && (root == "" || !strings.HasPrefix(r.Pos, root+string(filepath.Separator))) {
			continue
		}
		k := r.Pos + r.Kind + r.Where
		if seen[k] {
			continue // generic instances and split modules repeat a site
		}
		seen[k] = true
		recs = append(recs, r)
	}
	if sc.Err() != nil {
		return // unreadable, as when it cannot be opened
	}
	sort.SliceStable(recs, func(i, j int) bool { return recs[i].Pos < recs[j].Pos })
	counts := map[string]int{}
	loops := 0
	var ds []diag.Diagnostic
	for _, r := range recs {
		counts[r.Where]++
		if r.Where != "heap" && r.Where != "region" {
			continue
		}
		if r.Loop && r.Where == "heap" {
			loops++
		}
		what := r.Kind
		if r.Type != "" {
			what += " of " + "`" + r.Type + "`"
		}
		if r.Bytes > 0 {
			what += fmt.Sprintf(" (%d bytes)", r.Bytes)
		}
		d := diag.Diagnostic{Kind: "diagnostic", Severity: diag.Note, Code: diag.CodeHeapAlloc,
			Message: r.Where + " allocation: " + what + " in " + r.Fn, Primary: r.Reason}
		if r.Where == "region" {
			d.Primary = "in the caller's region, freed when the caller is done"
		}
		if file, line, col, ok := splitPos(r.Pos); ok {
			d.Span = diag.Span{File: file, Line: line, Col: col}
			d.Primary = strings.TrimSuffix(d.Primary, fmt.Sprintf(" (line %d)", line))
		}
		if r.Loop && r.Where == "heap" {
			d.Notes = append(d.Notes, "in a loop: one allocation per iteration")
		}
		if !r.Final {
			d.Notes = append(d.Notes, "the back end may still move it to the stack (go dialect)")
		}
		ds = append(ds, d)
	}
	ds = diag.Finish(ds, diag.NewSources(), diag.Options{Dir: rep.Dir, Context: rep.Mode == diag.ModeJSON, Rendered: rep.Mode == diag.ModeJSON})
	summary := fmt.Sprintf("%d allocation sites: %d on the heap", len(recs), counts["heap"])
	if loops > 0 {
		summary += fmt.Sprintf(" (%d in loops)", loops)
	}
	summary += fmt.Sprintf(", %d on the stack", counts["stack"])
	if counts["region"] > 0 {
		summary += fmt.Sprintf(", %d in regions", counts["region"])
	}
	if counts["static"] > 0 {
		summary += fmt.Sprintf(", %d static", counts["static"])
	}
	switch rep.Mode {
	case diag.ModeJSON:
		diag.WriteJSON(rep.W, ds)
		enc := json.NewEncoder(rep.W)
		enc.Encode(map[string]any{"schema": diag.Schema, "kind": "allocs", "sites": len(recs), "heap": counts["heap"], "heapInLoops": loops,
			"stack": counts["stack"], "region": counts["region"], "static": counts["static"]})
	default:
		st := diag.Plain
		if rep.Color {
			st = diag.Colors
		}
		h := &diag.Human{W: rep.W, Style: st, Dir: rep.Dir, Sources: diag.NewSources()}
		for i := range ds {
			if i > 0 {
				fmt.Fprintln(rep.W)
			}
			h.RenderOne(&ds[i])
		}
		if len(ds) > 0 {
			fmt.Fprintln(rep.W)
		}
		fmt.Fprintf(rep.W, "%s%s%s\n", st.Bold, summary, st.Reset)
	}
}

func splitPos(p string) (string, int, int, bool) {
	i := strings.LastIndexByte(p, ':')
	if i < 0 {
		return "", 0, 0, false
	}
	j := strings.LastIndexByte(p[:i], ':')
	if j < 0 {
		return "", 0, 0, false
	}
	line, err1 := strconv.Atoi(p[j+1 : i])
	col, err2 := strconv.Atoi(p[i+1:])
	return p[:j], line, col, err1 == nil && err2 == nil
}
