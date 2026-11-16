package verify

import "testing"

// TestBudgetStopsRun checks that engine A stops once the work budget is
// spent: analyzeFunc reports such a function unknown, so a fixpoint that
// runs on wastes the time the budget is meant to bound.
func TestBudgetStopsRun(t *testing.T) {
	r := loadTestdata(t, "codec")
	var best *FuncResult
	for _, f := range r.Funcs {
		if best == nil || len(f.Fn.Blocks) > len(best.Fn.Blocks) {
			best = f
		}
	}
	fn := best.Fn
	if len(fn.Blocks) < 4 {
		t.Fatalf("%s has %d blocks, want a function with a loop", fn, len(fn.Blocks))
	}
	full := newEngine(fn, nil, 0)
	full.run(newState(newCtx()))
	cx := newCtx()
	cx.work = 0
	e := newEngine(fn, nil, 0)
	e.run(newState(cx))
	if cx.work >= 0 {
		t.Fatalf("%s: the budget was not spent (work %d)", fn, cx.work)
	}
	if e.visits >= full.visits {
		t.Errorf("%s: %d block visits with no budget, %d with the full budget: the run did not stop", fn, e.visits, full.visits)
	}
}
