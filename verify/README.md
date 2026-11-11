# verify: machine-checked proofs for marmot's optimizer rules

SPEC M9. This is not a verified compiler. Each proof below is about a small model of one
rule. The model is written down next to the proof; what ties it to the C++ or Go code is
stated for every item, and none of them claims more than is written here.

Check everything with `verify/run.sh` (on linux-host; the record) or `verify/run.sh -l`
(quick local check). See `docs/verify.md` for the design and the findings.

Tools:

- Bend 2 at bend2go commit `054c774` (copy in `~/marmot/verify-tools/bend` on linux-host).
  Each `.bend` file is checked by bend2 and then, with `--verdict`, by the BendTT kernel
  (`bend2/bendtt.lean`, compiled with Lean 4.34.0), whose soundness is proved in Lean.
- Lean 4.34.0 (linux-host, the record) and 4.34.1 (laptop, quick checks), core library only
  (no Mathlib). Every theorem prints its axioms with `#print axioms`.

## What is proved

| Item | Rule (where it is written) | Model | Proved | Tool | Tie to the code |
| --- | --- | --- | --- | --- | --- |
| B1 | go-bce B1 (docs/passes.md) | 64-bit words as Nats with abstract `half`, `mod` | constants with `0 <= i < n` signed satisfy the check `i <u n` | Bend 2 (`b2.bend`, `b1`) | certificate checking: `verify/check` re-checks each B1 removal BCE.cpp reports, on real output |
| B2 | go-bce B2 | CFG walks; executions as traces of block visits with SSA values (`b2.bend`) | a check dominated by a passed check of the same index and length cannot fail | Bend 2 (`b2`) | certificate checking (`verify/check`, the named dominating check) |
| N2 | go-bce N2 | as B2 with index 0 and the pointer as length | a dominated nil check of the same pointer cannot fail | Bend 2 (`b2`, instance) | certificate checking |
| B3 | go-bce B3 | CFG as an edge list; dominance by checked certificates; executions as traces (`b3.bend`) | at every visit of the check block, `i < n` unsigned, for both `slt` and `ult` guards, any number of `add %i, 1` back edges | Bend 2 (`b3`) | certificate checking: `verify/check` re-checks every B3 condition, with dominance decided by the same closed-set certificate the proof assumes |
| T1, T2, T3 | path facts B2/B3 use (`cfg.bend`) | walks of any CFG with an abstract edge relation | t after the last h; d not re-entered after the last h; d not re-entered after the last k | Bend 2 | used by B2, B3 |
| V | Rule V (docs/loops.md), Versioning.cpp, Affine.cpp | affine checked values over the iteration space, wrapping words as Ints; loop bodies as instruction lists (`RuleV.lean`) | the precondition (term and lo/hi bounds, max < min) implies every check passes in every iteration; the versioned loop equals the original, panics included | Lean (`stay_sound`, `nil_sound`, `versioned_eq`) | none yet |
| P | Rule P clause 5 (independence) | iterations as lists of steps with read and write sets (`RuleP.lean`) | every interleaving of the iterations' steps ends with the sequential loop's memory; same-base windows with the clause 5 inequalities are disjoint | Lean (`schedule_eq`, `windows_disjoint`) | none yet |
| R | Rule P clause 3 (reductions) | folds over lists (`RuleR.lean`) | chunk partials from the identity combined in any order give the serial result, for add, sub, xor, or, and, smin, smax, umin, umax on words; binary64 add is not associative | Lean (`split_eq`, `*_ok`); the float fact by `native_decide` (trusts Lean's compiler and the FPU) | none |
| Escape | go-escape use rules (docs/passes.md) | provenance semantics: each instruction kind of the use table, any execution path (`Escape.lean`) | if the taint closure reaches no store value, sink or non-noescape call, no memory cell and no sink ever holds the allocation's address | Lean (`no_escape`) | none; the go.noescape callee contract is assumed; the cycle (slot reuse) rule and size limits are not proved |
| Region | region inference (docs/frontend-passes.md) | one-object pointers; the call's block with region allocations; may/must sets (`Region.lean`) | from a fresh state, every name holding a region pointer is confined, every memory cell holding one is inside a region object, no sink receives one | Lean (`confined`, `init_inv`) | none; the noescape callee contract and the pass's last-use computation are assumed |
| Defer | open-coded defers (docs/frontend-passes.md) | paths in an acyclic region with a topological numbering; the runner as single steps over a pending bitmask (`Defer.lean`) | defers along any path have increasing numbers (descending = LIFO); under any interruptions the runner makes exactly the pending calls, each once, highest first | Lean (`path_increasing`, `runner_order`, `runner_once`) | none; the reverse postorder numbering and the runtime's re-entry on panic/Goexit are assumed |
| SliceGrow | runtime slice growth (lib/src/runtime/slice.go) | nextslicecap with wrapping 64-bit ints, loop with fuel (`SliceGrow.lean`) | every exit returns a capacity >= newLen; the rounded capacity is >= the requested one | Lean | the program `testdata/progs/appendcap.go` (cap >= len, elements kept); roundupsize >= size is assumed |
| Ring | buffered channel ring (lib/src/runtime/chanring.go, ringPush, ringPop, close bit) | step-by-step transcription, one step per atomic op, the element copy split in two; sequential consistency (`model/ring_test.go`) | exhaustive over all interleavings for capacities 1 to 3, up to 3 senders and receivers: no torn or stale read, no overwrite of a full slot, each value received at most once and as sent at its position, per-sender FIFO per receiver; the original single encoding is caught at capacity 1 | Go test (explicit-state model check), not a proof | the transcription is by hand |
| Wake | buffered channel blocking (chanring.go, chansendBuf, chanrecvBuf, closechanBuf) | ring operations atomic, c.lock, both wait queues, notify after success, retry after enqueue (`model/wake_test.go`) | exhaustive: with balanced sends and receives every goroutine finishes (no lost wake-up); with a close at any moment no goroutine stays parked and receivers drain every element sent before it; without the retry a wake-up is lost | Go test (model check) | transcription by hand; select is not modeled |
| MapMatch | map control word matches (lib/src/runtime/map.go) | 64-bit words, the control byte invariant (`MapMatch.lean`) | matchEmpty and matchFull are exact; matchH2 reports every byte equal to h2 and never an empty or deleted slot | Lean `bv_decide` (trusts Lean's compiler for the LRAT check) | the functions are transcribed by hand; growth is not covered |
| Probe | map probing order (lib/src/runtime/map.go) | probe i at (h + i(i+1)/2) mod 2^k (`Probe.lean`) | the first 2^k probes are pairwise different: every group is visited exactly once | Lean (`probe_injective`) | none (the probe sequence is read from the code by hand) |
| STW | stop-the-world with gc_mu and worldsema, safepoints, SIGURG acks, syscall entry and exit, REQ_EXITSYSCALL, sysmon retake, wakep and spinners (runtime/core sched.c, gc.c, lock.c, signal.c at 5966c50) (`model/stw_test.go`) | goroutines run scripts of GCs and syscalls; spinner's findrunnable is one step; local run queues empty; GC helpers not modeled | check that the end stays reachable from every state: at HEAD a lost wake-up (syscall exit queues its goroutine after start_the_world decided there was no work; reported to rtcore); with wakep after the queueing, no stuck state | Go test (model check) | transcription by hand |
| Mark | parallel marking termination and GC helper start/stop (gc.c drain_parallel, mark_parallel, gc_helper, helpers_start, helpers_wait) (`model/mark_test.go`) | abstract work units, share to one wbuf queue, helpers joining late | when wdone is set no work is left anywhere; the end of marking (collector past helpers_wait, helpers asleep) stays reachable; a mutant without wnidle-- is caught | Go test (model check) | transcription by hand; the 200us sleep and spin are one blocked step |
| Poly | Rule P clause 2 (polynomial inductions) | Nat and Int arithmetic (`Poly.lean`) | closed form equals the serial recurrence; halving in 64 bits is exact mod 2^N; halving after truncating to N bits is wrong | Lean | the counterexample reproduces on real output: `testdata/progs/polyinduct16.go` |

Not proved yet: N1 (it is a fact about the runtime and LLVM: allocation results, `alloca`
and global addresses are never null), Rule P rows and run time range checks, Rule V loaded
bounds, Rules I, T, M, loop-jam, the escape cycle rule, the runtime
models. See docs/verify.md for the order of work.

## Status of findings

- Rule P polynomial inductions narrower than 64 bits (`testdata/progs/polyinduct16.go`): fixed
  at HEAD by commit df62fdf (loops, "exact polynomial inductions below 64 bits"), which
  computes binomial(k, 2) from the 64-bit counter as `Poly.binom_halving` proves. The
  program prints the same as stock Go under dev, go dialect and -par (`progs.sh`).

## Rule P claims the checker cannot re-check

In `backend/test/loops/Inputs/par.go` the checker reports two claims as not re-checkable,
both a store paired with itself under reason `window`:

- `main.escape`: the store `out[py*w+px]` in the loop over `px`;
- `main.rows`: the store `row[c]` (`g[r*w + c]`) in the loop over `c`.

In both the loop's step is a constant (coef 4 bytes) but the offset range [lo, hi] is the
row start `4*w*py` (or `4*w*r`), which depends on the outer loop and on `w`, so the report
says `"lo":"sym","hi":"sym"`. For a self pair the proved condition
(`RuleP.windows_disjoint`) needs only `hi - lo + size <= |coef|`, that is `hi - lo`, which
is 0 here (lo and hi are the same expression). To make such claims checkable the report
needs `span` = hi - lo whenever ScalarEvolution proves it a constant (and, for pairs of
different accesses, `lo` and `hi` as differences from one shared symbolic start, or as
affine forms over named invariants so the checker can cancel them). Requested from loops.

## Certificate checking (go-bce)

`marmot-opt --go-bce='report=FILE'` writes one line per removed check: function, walk index,
rule, and for B2/N2 the dominating check (BCE.cpp, docs/passes.md). `verify/check` (plain Go,
no LLVM) reads the MLIR before and after the pass and the report, validates the named rule
for every removal with its own implementation of the rules and its own dominance
computation, and confirms that the report names exactly the checks that disappeared.
`verify/check/corpus.sh` runs it over conformance programs; `go test ./verify/check` has
negative cases that must be rejected. What this ties: the rule applications BCE.cpp makes on
those programs are instances of the proved rules. It does not show BCE.cpp is right on other
inputs.

## Files

- `bend/nat.bend`, `bend/list.bend`: Nat order and list lemmas.
- `bend/cfg.bend`: walks, dominance, T1, T2, T3.
- `bend/graph.bend`: concrete CFGs, the "only predecessor" check, dominance certificates.
- `bend/word.bend`: words, signed compare, wrapping +1.
- `bend/b2.bend`, `bend/b3.bend`: go-bce rules.
- `lean/RuleV.lean`, `lean/RuleP.lean`, `lean/RuleR.lean`, `lean/Poly.lean`: loop rules.
- `check/`: the go-bce certificate checker and `corpus.sh`.
- `testdata/progs/`: programs generated from the proofs' side conditions, with expected
  output from stock Go; `progs.sh` runs them under marmot (dev, go dialect, -par).
