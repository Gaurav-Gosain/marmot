package smt

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
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
	key := s.cacheKey(Script(asserts, s.RLimit))
	if r, ok := s.cacheGet(key); ok {
		return r
	}
	r := s.check(ctx, asserts)
	if r.Answer != Unknown {
		s.cachePut(key, r)
	}
	return r
}

func (s *Solver) check(ctx context.Context, asserts []*T) Result {
	sels := selects(asserts)
	p := newPrinter(append(append([]*T(nil), asserts...), sels...))
	p.header(asserts, s.RLimit)
	for _, a := range asserts {
		p.assert(a)
	}
	// The index and the byte of every select, as named values.
	for i, t := range sels {
		idx, v := p.term(t.Args[1]), p.term(t)
		fmt.Fprintf(&p.b, "(define-fun sel%d_i () (_ BitVec 64) %s)\n(define-fun sel%d_v () (_ BitVec 8) %s)\n", i, idx, i, v)
	}
	p.b.WriteString("(check-sat)\n")
	if bvs := varNames(asserts); len(bvs) > 0 {
		fmt.Fprintf(&p.b, "(get-value (%s))\n", strings.Join(bvs, " "))
	}
	if len(sels) > 0 {
		var names []string
		for i := range sels {
			names = append(names, fmt.Sprintf("sel%d_i sel%d_v", i, i))
		}
		fmt.Fprintf(&p.b, "(get-value (%s))\n", strings.Join(names, " "))
	}
	script := p.b.String()
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

// CheckMany checks base together with each query in one solver process:
// declarations and base once, then a push, the query, check-sat, the
// values, and a pop per query.
func (s *Solver) CheckMany(ctx context.Context, base []*T, queries []*T) []Result {
	// Answers come from the cache when every query is there.
	keys := make([]string, len(queries))
	cached := make([]Result, len(queries))
	all := true
	for i, q := range queries {
		keys[i] = s.cacheKey(Script(append(append([]*T(nil), base...), q), s.RLimit))
		if r, ok := s.cacheGet(keys[i]); ok {
			cached[i] = r
		} else {
			all = false
		}
	}
	if all {
		return cached
	}
	res := s.checkMany(ctx, base, queries)
	for i, r := range res {
		if r.Answer != Unknown {
			s.cachePut(keys[i], r)
		}
	}
	return res
}

// The answer cache: deterministic answers (rlimit, not the wall clock)
// of a solver version for a script, one small file each.
func (s *Solver) cacheKey(script string) string {
	h := sha256.Sum256([]byte(s.Version + "\x00" + strconv.Itoa(s.RLimit) + "\x00" + script))
	return hex.EncodeToString(h[:16])
}

func cacheDir() string {
	if d := os.Getenv("MARMOT_VERIFY_SMT_CACHE"); d != "" {
		return d
	}
	d, err := os.UserCacheDir()
	if err != nil {
		return ""
	}
	return filepath.Join(d, "marmot", "verify-smt")
}

func (s *Solver) cacheGet(key string) (Result, bool) {
	dir := cacheDir()
	if dir == "" {
		return Result{}, false
	}
	b, err := os.ReadFile(filepath.Join(dir, key[:2], key))
	if err != nil {
		return Result{}, false
	}
	var r Result
	if json.Unmarshal(b, &r) != nil {
		return Result{}, false
	}
	return r, true
}

func (s *Solver) cachePut(key string, r Result) {
	dir := cacheDir()
	if dir == "" {
		return
	}
	b, err := json.Marshal(r)
	if err != nil {
		return
	}
	p := filepath.Join(dir, key[:2], key)
	if os.MkdirAll(filepath.Dir(p), 0o755) != nil {
		return
	}
	tmp := p + ".tmp" + strconv.Itoa(os.Getpid())
	if os.WriteFile(tmp, b, 0o644) == nil {
		_ = os.Rename(tmp, p)
	}
}

func (s *Solver) checkMany(ctx context.Context, base []*T, queries []*T) []Result {
	all := append(append([]*T(nil), base...), queries...)
	sels := make([][]*T, len(queries))
	for i, q := range queries {
		sels[i] = selects([]*T{q})
		all = append(all, sels[i]...)
	}
	// The declarations of every variable, then base, then the queries.
	// Definitions a query needs are made inside its push, so they go
	// with its pop.
	p := newPrinter(all)
	p.header(all, s.RLimit)
	for _, a := range base {
		p.assert(a)
	}
	b := &p.b
	for i, q := range queries {
		vars := varNames(append(append([]*T(nil), base...), q))
		m := p.mark()
		b.WriteString("(push 1)\n")
		for k, t := range sels[i] {
			idx, v := p.term(t.Args[1]), p.term(t)
			fmt.Fprintf(b, "(declare-const sel%d_i (_ BitVec 64))\n(declare-const sel%d_v (_ BitVec 8))\n(assert (= sel%d_i %s))\n(assert (= sel%d_v %s))\n", k, k, k, idx, k, v)
			vars = append(vars, fmt.Sprintf("sel%d_i", k), fmt.Sprintf("sel%d_v", k))
		}
		p.assert(q)
		b.WriteString("(check-sat)\n")
		if len(vars) > 0 {
			fmt.Fprintf(b, "(get-value (%s))\n", strings.Join(vars, " "))
		}
		b.WriteString("(pop 1)\n")
		p.pop(m)
	}
	ctx, cancel := context.WithTimeout(ctx, s.Wall*time.Duration(1+len(queries)/4))
	defer cancel()
	cmd := exec.CommandContext(ctx, s.Path, "-in", "-smt2")
	cmd.Stdin = strings.NewReader(b.String())
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	_ = cmd.Run()
	// Answers in order; a sat answer is followed by its values. A
	// get-value after unsat or unknown prints an error line, skipped.
	res := make([]Result, len(queries))
	for i := range res {
		res[i] = Result{Answer: Unknown, Reason: "no answer"}
	}
	lines := strings.Split(out.String(), "\n")
	qi := 0
	for li := 0; li < len(lines) && qi < len(queries); li++ {
		l := strings.TrimSpace(lines[li])
		switch l {
		case "sat", "unsat", "unknown":
		default:
			continue
		}
		r := Result{Answer: Answer(l)}
		if l == "unknown" {
			r.Reason = "budget"
		}
		if l == "sat" {
			// The values follow, possibly over several lines, up to the
			// next answer.
			var vals strings.Builder
			for li+1 < len(lines) {
				n := strings.TrimSpace(lines[li+1])
				if n == "sat" || n == "unsat" || n == "unknown" {
					break
				}
				vals.WriteString(n + " ")
				li++
			}
			m := map[string]uint64{}
			for _, g := range reValue.FindAllStringSubmatch(vals.String(), -1) {
				if v, err := parseValue(g[2], g[3]); err == nil {
					m[g[1]] = v
				}
			}
			for k, t := range sels[qi] {
				idx, ok1 := m[fmt.Sprintf("sel%d_i", k)]
				v, ok2 := m[fmt.Sprintf("sel%d_v", k)]
				if ok1 && ok2 {
					m[fmt.Sprintf("%s[%d]", t.Args[0].Name, idx)] = v
				}
			}
			r.Model = m
		}
		res[qi] = r
		qi++
	}
	return res
}

func varNames(ts []*T) []string {
	seen := map[string]bool{}
	visited := map[*T]bool{}
	var out []string
	var walk func(t *T)
	walk = func(t *T) {
		if visited[t] {
			return
		}
		visited[t] = true
		if t.Op == OpVar && !seen[t.Name] {
			seen[t.Name] = true
			out = append(out, t.Name)
		}
		for _, a := range t.Args {
			walk(a)
		}
	}
	for _, t := range ts {
		walk(t)
	}
	slices.Sort(out)
	return out
}
