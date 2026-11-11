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
| Poly | Rule P clause 2 (polynomial inductions) | Nat and Int arithmetic (`Poly.lean`) | closed form equals the serial recurrence; halving in 64 bits is exact mod 2^N; halving after truncating to N bits is wrong | Lean | the counterexample reproduces on real output: `testdata/progs/polyinduct16.go` |

Not proved yet: N1 (it is a fact about the runtime and LLVM: allocation results, `alloca`
and global addresses are never null), Rule P rows and run time range checks, Rule V loaded
bounds, Rules I, T, M, loop-jam, escape analysis, regions, open-coded defers, the runtime
models. See docs/verify.md for the order of work.

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
