package check

func runCross(sc *scope, opts Options, st *store, r *CheckResult) {
	r.Status = Skipped
	r.Notes = append(r.Notes, "not implemented yet")
}

func runDeadcode(sc *scope, opts Options, st *store, r *CheckResult) {
	r.Status = Skipped
	r.Notes = append(r.Notes, "not implemented yet")
}

func runVuln(sc *scope, opts Options, st *store, r *CheckResult) {
	r.Status = Skipped
	r.Notes = append(r.Notes, "not implemented yet")
}
