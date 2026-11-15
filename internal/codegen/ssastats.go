package codegen

import (
	"fmt"
	"go/token"
	"io"
	"os"
	"sort"
	"sync"

	"golang.org/x/tools/go/ssa"
)

// MARMOT_SSASTATS=FILE (a measurement aid): for each function and kind of
// SSA instruction, how many instructions, and how many MLIR values and
// blocks codegen emitted for them, appended to FILE as tab separated lines
// (package, symbol, kind, instructions, values, blocks) when the driver
// calls WriteSSAStats.
var ssaStatsOn = os.Getenv("MARMOT_SSASTATS") != ""

type ssaStat struct{ n, vals, blocks int }

var ssaStats struct {
	sync.Mutex
	m map[[3]string]*ssaStat
}

// noteSSA adds one instruction of kind k with its emitted values and blocks.
func (f *funcGen) noteSSA(k string, vals, blocks int) {
	pkg := "?"
	if f.fn != nil && f.fn.Pkg != nil {
		pkg = f.fn.Pkg.Pkg.Path()
	}
	ssaStats.Lock()
	defer ssaStats.Unlock()
	if ssaStats.m == nil {
		ssaStats.m = map[[3]string]*ssaStat{}
	}
	key := [3]string{pkg, f.statSym, k}
	s := ssaStats.m[key]
	if s == nil {
		s = &ssaStat{}
		ssaStats.m[key] = s
	}
	s.n++
	s.vals += vals
	s.blocks += blocks
}

// ssaKind names an instruction for the stats: its type, with the operator,
// call form or checked value's origin where that tells constructs apart.
func ssaKind(instr ssa.Instruction) string {
	k := fmt.Sprintf("%T", instr)[5:]
	switch v := instr.(type) {
	case *ssa.UnOp:
		k += " " + v.Op.String()
		if v.Op == token.MUL {
			k += " of " + fmt.Sprintf("%T", v.X)[5:]
		}
	case *ssa.BinOp:
		k += " " + v.Op.String()
	case *ssa.Call:
		switch {
		case v.Call.IsInvoke():
			k += " invoke"
		case v.Call.StaticCallee() != nil:
			k += " static"
		default:
			if _, ok := v.Call.Value.(*ssa.Builtin); ok {
				k += " builtin " + v.Call.Value.Name()
			} else {
				k += " dynamic"
			}
		}
	case *ssa.FieldAddr:
		k += " of " + fmt.Sprintf("%T", v.X)[5:]
	case *ssa.Store:
		k += " to " + fmt.Sprintf("%T", v.Addr)[5:]
	}
	return k
}

// WriteSSAStats appends the collected stats to the MARMOT_SSASTATS file.
func WriteSSAStats() {
	name := os.Getenv("MARMOT_SSASTATS")
	if name == "" {
		return
	}
	ssaStats.Lock()
	defer ssaStats.Unlock()
	keys := make([][3]string, 0, len(ssaStats.m))
	for k := range ssaStats.m {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		a, b := keys[i], keys[j]
		if a[0] != b[0] {
			return a[0] < b[0]
		}
		if a[1] != b[1] {
			return a[1] < b[1]
		}
		return a[2] < b[2]
	})
	w, err := os.OpenFile(name, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer w.Close()
	for _, k := range keys {
		s := ssaStats.m[k]
		fmt.Fprintf(io.Writer(w), "%s\t%s\t%s\t%d\t%d\t%d\n", k[0], k[1], k[2], s.n, s.vals, s.blocks)
	}
	ssaStats.m = nil
}
