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
// compiled program prints as prog (so a matcher generated against another
// version of regexp/syntax is never used).
func MarmotMustCompile(expr string, m func(s string, pos, ncap int, dstCap []int) ([]int, bool), prog string) *Regexp {
	re := MustCompile(expr)
	if re.prog.String() == prog {
		re.marmot = m
	}
	return re
}

// MarmotCompile is Compile with a matcher, as MarmotMustCompile.
func MarmotCompile(expr string, m func(s string, pos, ncap int, dstCap []int) ([]int, bool), prog string) (*Regexp, error) {
	re, err := Compile(expr)
	if err == nil && re.prog.String() == prog {
		re.marmot = m
	}
	return re, err
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
