// Entry points for marmot's compile time regexp specialization
// (internal/specialize). For a constant pattern the compiler generates a
// matcher from the same syntax.Prog that Compile builds and attaches it here.
// The matcher computes what the general engines compute (leftmost-first
// matches) and reports false when it gives up, in which case find runs the
// general engines. User code must not call these.

package regexp

import (
	"regexp/syntax"
	"sync"
	"unicode/utf8"
)

// MarmotMustCompile is MustCompile that attaches matcher m when the
// compiled program hashes to prog (so a matcher generated against another
// version of regexp/syntax is never used).
func MarmotMustCompile(expr string, m func(s string, pos, ncap int, dstCap []int) ([]int, bool), prog uint64) *Regexp {
	re := MustCompile(expr)
	if marmotProgHash(re.prog) == prog {
		re.marmot = m
	}
	return re
}

// MarmotCompile is Compile with a matcher, as MarmotMustCompile.
func MarmotCompile(expr string, m func(s string, pos, ncap int, dstCap []int) ([]int, bool), prog uint64) (*Regexp, error) {
	re, err := Compile(expr)
	if err == nil && marmotProgHash(re.prog) == prog {
		re.marmot = m
	}
	return re, err
}

// marmotProgHash is FNV-1a over every field of the program. The compiler
// computes the same hash (internal/specialize progHash) from the program it
// generated the matcher from.
func marmotProgHash(p *syntax.Prog) uint64 {
	h := uint64(14695981039346656037)
	mix := func(v uint64) {
		for i := 0; i < 8; i++ {
			h ^= v & 0xff
			h *= 1099511628211
			v >>= 8
		}
	}
	mix(uint64(p.Start))
	mix(uint64(p.NumCap))
	mix(uint64(len(p.Inst)))
	for i := range p.Inst {
		in := &p.Inst[i]
		mix(uint64(in.Op))
		mix(uint64(in.Out))
		mix(uint64(in.Arg))
		mix(uint64(len(in.Rune)))
		for _, r := range in.Rune {
			mix(uint64(r))
		}
	}
	return h
}

// MarmotEmpty reports whether the empty-width assertions op hold at pos.
func MarmotEmpty(s string, pos int, op syntax.EmptyOp) bool {
	r1, r2 := endOfText, endOfText
	if uint(pos-1) < uint(len(s)) {
		r1, _ = utf8.DecodeLastRuneInString(s[:pos])
	}
	if uint(pos) < uint(len(s)) {
		r2, _ = utf8.DecodeRuneInString(s[pos:])
	}
	return newLazyFlag(r1, r2).match(op)
}

// MarmotJob is a backtracking job of a generated matcher: an alternative to
// resume (PC >= 0) or a capture slot to restore (PC = -1 - slot).
type MarmotJob struct {
	PC  int
	Pos int
}

var marmotJobs = sync.Pool{New: func() any { s := make([]MarmotJob, 0, 256); return &s }}

// MarmotGetJobs returns an empty job stack for a matcher call.
func MarmotGetJobs() *[]MarmotJob {
	p := marmotJobs.Get().(*[]MarmotJob)
	*p = (*p)[:0]
	return p
}

// MarmotPutJobs returns the job stack j (grown from p) to the pool.
func MarmotPutJobs(p *[]MarmotJob, j []MarmotJob) {
	if cap(j) <= 1<<16 {
		*p = j[:0]
		marmotJobs.Put(p)
	}
}
