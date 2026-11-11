package main

// mlir.go: a small reader for the MLIR text marmot emits (llvm dialect plus go dialect ops),
// enough for the go-bce rules: functions, blocks, block arguments, ops with their results,
// operands and successors. It is independent of LLVM and of the backend.

import (
	"bufio"
	"fmt"
	"io"
	"regexp"
	"strings"
)

type Func struct {
	Name   string
	Blocks []*Block
	ByName map[string]*Block
	Def    map[string]*Op    // SSA name -> defining op (nil for block and function args)
	ArgOf  map[string]*Block // block argument name -> its block
	ArgIdx map[string]int    // block argument name -> its position
	FArg   map[string]int    // function argument name -> its position
}

type Block struct {
	Name  string
	Args  []string
	Ops   []*Op
	Succs []Succ // from the terminator
	Preds []*Block
	Index int
}

type Succ struct {
	Block    string
	Operands []string
}

type Op struct {
	Results  []string
	Name     string // e.g. llvm.icmp, go.index
	Text     string // the op text after the name, without the trailing loc(...)
	Operands []string
	Block    *Block
	Pos      int // position in its block
}

var (
	reFunc    = regexp.MustCompile(`^\s*llvm\.func\s+(?:\w+\s+)*@("(?:[^"\\]|\\.)*"|[\w.$]+)\(`)
	reLabel   = regexp.MustCompile(`^\s*\^([\w.$-]+)(?:\((.*)\))?:\s*$`)
	reOp      = regexp.MustCompile(`^\s*(?:((?:%[\w.$-]+(?::\d+)?)(?:\s*,\s*%[\w.$-]+)*)\s*=\s*)?"?([a-z_][\w.]*)"?(.*)$`)
	reValue   = regexp.MustCompile(`%[\w.$-]+`)
	reSucc    = regexp.MustCompile(`\^([\w.$-]+)(\(([^()]*)\))?`)
	reLoc     = regexp.MustCompile(`\s+loc\([^()]*(\([^()]*\))*[^()]*\)\s*$`)
	reArgName = regexp.MustCompile(`(%[\w.$-]+)\s*:`)
)

// Parse reads all functions with bodies.
func Parse(r io.Reader) ([]*Func, error) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 1<<20), 1<<26)
	var fns []*Func
	var f *Func
	var b *Block
	depth := 0
	for sc.Scan() {
		line := sc.Text()
		if i := strings.Index(line, "  // "); i >= 0 {
			line = line[:i]
		}
		if f == nil {
			m := reFunc.FindStringSubmatch(line)
			if m == nil || !strings.HasSuffix(strings.TrimSpace(line), "{") {
				continue
			}
			f = &Func{Name: strings.Trim(m[1], `"`), ByName: map[string]*Block{}, Def: map[string]*Op{},
				ArgOf: map[string]*Block{}, ArgIdx: map[string]int{}, FArg: map[string]int{}}
			hdr := line[len(m[0]):]
			if k := strings.Index(hdr, ") "); k >= 0 {
				hdr = hdr[:k]
			}
			for i, a := range reArgName.FindAllStringSubmatch(hdr, -1) {
				f.FArg[a[1]] = i
			}
			b = &Block{Name: "entry"}
			f.Blocks = append(f.Blocks, b)
			f.ByName[b.Name] = b
			depth = 1
			continue
		}
		t := strings.TrimSpace(line)
		// Ops with regions are not used by the go-bce rules; skip their bodies but keep
		// track of nesting so the function end is found.
		if depth > 1 {
			depth += strings.Count(t, "{") - strings.Count(t, "}")
			continue
		}
		if strings.HasPrefix(t, "}") {
			fns = append(fns, f)
			f, b = nil, nil
			continue
		}
		if m := reLabel.FindStringSubmatch(line); m != nil {
			b = &Block{Name: m[1], Index: len(f.Blocks)}
			for i, a := range reArgName.FindAllStringSubmatch(m[2], -1) {
				b.Args = append(b.Args, a[1])
				f.ArgOf[a[1]] = b
				f.ArgIdx[a[1]] = i
			}
			f.Blocks = append(f.Blocks, b)
			f.ByName[b.Name] = b
			continue
		}
		if t == "" || strings.HasPrefix(t, "//") {
			continue
		}
		if strings.HasSuffix(t, "{") && !strings.HasSuffix(t, "}") {
			depth += strings.Count(t, "{") - strings.Count(t, "}")
		}
		m := reOp.FindStringSubmatch(t)
		if m == nil {
			return nil, fmt.Errorf("%s: cannot read %q", f.Name, t)
		}
		op := &Op{Name: m[2], Text: reLoc.ReplaceAllString(m[3], ""), Block: b, Pos: len(b.Ops)}
		if m[1] != "" {
			for _, r := range strings.Split(m[1], ",") {
				r = strings.TrimSpace(r)
				if i := strings.Index(r, ":"); i >= 0 {
					r = r[:i]
				}
				op.Results = append(op.Results, r)
				f.Def[r] = op
			}
		}
		body := op.Text
		// operands: the values before the type list (the first " : " at nesting level 0)
		if i := typeColon(body); i >= 0 && !strings.HasPrefix(op.Name, "llvm.cond_br") && !strings.HasPrefix(op.Name, "llvm.br") {
			body = body[:i]
		}
		if op.Name == "llvm.br" || op.Name == "llvm.cond_br" || op.Name == "llvm.switch" {
			for _, s := range reSucc.FindAllStringSubmatch(op.Text, -1) {
				sc := Succ{Block: s[1]}
				if s[3] != "" {
					ops := s[3]
					if i := strings.Index(ops, ":"); i >= 0 {
						ops = ops[:i]
					}
					sc.Operands = reValue.FindAllString(ops, -1)
				}
				b.Succs = append(b.Succs, sc)
			}
			body = reSucc.ReplaceAllString(body, "")
		}
		op.Operands = reValue.FindAllString(body, -1)
		b.Ops = append(b.Ops, op)
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	for _, f := range fns {
		for _, b := range f.Blocks {
			for _, s := range b.Succs {
				if t := f.ByName[s.Block]; t != nil {
					t.Preds = append(t.Preds, b)
				}
			}
		}
	}
	return fns, nil
}

// typeColon finds the " : " that starts an op's type list, outside brackets and quotes.
func typeColon(s string) int {
	depth := 0
	inq := false
	for i := 0; i < len(s); i++ {
		switch c := s[i]; {
		case c == '"':
			inq = !inq
		case inq:
		case c == '(' || c == '[' || c == '{' || c == '<':
			depth++
		case c == ')' || c == ']' || c == '}' || c == '>':
			depth--
		case c == ':' && depth == 0 && i > 0 && s[i-1] == ' ' && i+1 < len(s) && s[i+1] == ' ':
			return i
		}
	}
	return -1
}

// Const returns the integer value of an `llvm.mlir.constant(N : iK)`, and its type.
var reConst = regexp.MustCompile(`^\s*\((-?\d+)\s*:\s*(i\d+)\)`)

func (f *Func) Const(v string) (int64, string, bool) {
	op := f.Def[v]
	if op == nil || op.Name != "llvm.mlir.constant" {
		return 0, "", false
	}
	m := reConst.FindStringSubmatch(op.Text)
	if m == nil {
		return 0, "", false
	}
	var n int64
	fmt.Sscan(m[1], &n)
	return n, m[2], true
}
