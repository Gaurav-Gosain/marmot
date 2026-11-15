package smt

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Answer of a solver run.
type Answer string

const (
	Sat     Answer = "sat"
	Unsat   Answer = "unsat"
	Unknown Answer = "unknown"
)

// Result is a solver answer, the model of a sat answer (bit-vector
// variables), and the solver's identity for cache keys.
type Result struct {
	Answer Answer
	Model  map[string]uint64
	Reason string // why unknown: budget, error text
}

// Solver runs an SMT-LIB2 solver on PATH (z3).
type Solver struct {
	Path    string
	Version string
	// RLimit is the deterministic resource limit per query (z3 rlimit);
	// Wall is a wall clock cap on top, for safety.
	RLimit int
	Wall   time.Duration
}

var (
	z3Once sync.Once
	z3     *Solver
)

// Z3 finds z3 on PATH; nil when it is not installed.
func Z3() *Solver {
	z3Once.Do(func() {
		p, err := exec.LookPath("z3")
		if err != nil {
			return
		}
		out, err := exec.Command(p, "--version").Output()
		if err != nil {
			return
		}
		z3 = &Solver{Path: p, Version: strings.TrimSpace(string(out)), RLimit: 2_000_000, Wall: 5 * time.Second}
	})
	return z3
}

var reValue = regexp.MustCompile(`\(\s*([A-Za-z_][A-Za-z0-9_]*)\s+(#x[0-9a-fA-F]+|#b[01]+|true|false|\(_ bv([0-9]+) [0-9]+\))\s*\)`)

// Check asserts the terms and asks for satisfiability. The model has the
// bit-vector variables and, under "arr[i]" names, the bytes the terms
// select.
func (s *Solver) Check(ctx context.Context, asserts []*T) Result {
	script := Script(asserts, s.RLimit)
	sels := selects(asserts)
	if len(sels) > 0 {
		// The index and the byte of every select, as named values.
		var gv strings.Builder
		for i, t := range sels {
			fmt.Fprintf(&gv, "(define-fun sel%d_i () (_ BitVec 64) %s)\n(define-fun sel%d_v () (_ BitVec 8) %s)\n", i, String(t.Args[1]), i, String(t))
		}
		var names []string
		for i := range sels {
			names = append(names, fmt.Sprintf("sel%d_i sel%d_v", i, i))
		}
		script = strings.Replace(script, "(check-sat)\n", gv.String()+"(check-sat)\n", 1) + "(get-value (" + strings.Join(names, " ") + "))\n"
	}
	ctx, cancel := context.WithTimeout(ctx, s.Wall)
	defer cancel()
	cmd := exec.CommandContext(ctx, s.Path, "-in", "-smt2")
	cmd.Stdin = strings.NewReader(script)
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	_ = cmd.Run()
	text := out.String()
	first, rest, _ := strings.Cut(strings.TrimSpace(text), "\n")
	switch strings.TrimSpace(first) {
	case "unsat":
		return Result{Answer: Unsat}
	case "sat":
		m := map[string]uint64{}
		for _, g := range reValue.FindAllStringSubmatch(rest, -1) {
			v, err := parseValue(g[2], g[3])
			if err == nil {
				m[g[1]] = v
			}
		}
		for i, t := range sels {
			idx, ok1 := m[fmt.Sprintf("sel%d_i", i)]
			v, ok2 := m[fmt.Sprintf("sel%d_v", i)]
			if ok1 && ok2 {
				m[fmt.Sprintf("%s[%d]", t.Args[0].Name, idx)] = v
			}
		}
		return Result{Answer: Sat, Model: m}
	case "unknown":
		return Result{Answer: Unknown, Reason: "budget"}
	}
	if ctx.Err() != nil {
		return Result{Answer: Unknown, Reason: "wall clock"}
	}
	return Result{Answer: Unknown, Reason: "solver: " + firstLine(text)}
}

func parseValue(lit, dec string) (uint64, error) {
	switch {
	case lit == "true":
		return 1, nil
	case lit == "false":
		return 0, nil
	case strings.HasPrefix(lit, "#x"):
		return strconv.ParseUint(lit[2:], 16, 64)
	case strings.HasPrefix(lit, "#b"):
		return strconv.ParseUint(lit[2:], 2, 64)
	case dec != "":
		return strconv.ParseUint(dec, 10, 64)
	}
	return 0, fmt.Errorf("value %q", lit)
}

func firstLine(s string) string {
	l, _, _ := strings.Cut(strings.TrimSpace(s), "\n")
	return l
}

// selects lists the distinct select terms of the assertions.
func selects(ts []*T) []*T {
	var out []*T
	seen := map[*T]bool{}
	var walk func(t *T)
	walk = func(t *T) {
		if seen[t] {
			return
		}
		seen[t] = true
		if t.Op == OpSelect && t.Args[0].Op == OpArrVar {
			out = append(out, t)
		}
		for _, a := range t.Args {
			walk(a)
		}
	}
	for _, t := range ts {
		walk(t)
	}
	return out
}
