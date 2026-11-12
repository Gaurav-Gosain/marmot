/-
Escape.lean: go-escape's use rules (backend/lib/Dialect/Go/Escape.cpp), on a provenance model.

Model. Every runtime value carries the set of allocations whose address it may contain (its
provenance). Instructions, one per row of the use table:
  alloc d            d := a fresh object; its provenance is {that allocation}
  derive d srcs      gep, go.index, go.slice, select, insertvalue/extractvalue, block argument
                     passing, go.make_iface data, go.box, go.make_closure capture, nocopy
                     bytes_to_str: d's provenance is within the union of the sources'
  load d addr        d := memory at addr (the address operand is harmless)
  store v addr       memory at addr := v (the address operand is harmless)
  pure d srcs        icmp, copying string ops, map key reads: d carries no provenance
  call d args        direct call whose parameters have go.noescape: by the summary contract the
                     callee neither keeps nor returns the arguments' provenance, and what it
                     writes to memory carries none of it
  sink v             everything else: return, ptrtoint, go.go, chan send, map update, indirect
                     and interface calls, call arguments without go.noescape, branch conditions
An execution is any sequence of instruction instances of the function (so any CFG path, any
number of loop iterations), SSA style: each instance writes its destination.

The analysis: the taint set T is closed under the rules (alloc's d is in T; a derive with a
tainted source has a tainted destination). The allocation does not escape when no store has
a tainted value operand, no sink has a tainted operand, and no non-pure, non-derive use is
of a tainted value except as a load or store address or a noescape call argument.

Theorem `no_escape`: then in every execution, every value whose provenance contains the
allocation is named in T, no memory cell ever holds such a value, and no sink receives one.
So nothing outside the frame can reach the object when the function returns, and it can live
on the stack. Trusted: the summary contract of go.noescape callees (for functions in the
module it is the greatest fixpoint the pass computes; for declarations it is the frontend's
or a previous compile's promise).

Not proved: stack slot reuse in a cycle (the pass's cycle rule). The argument on paper: under
the cycle rule no tainted value reaches a block argument, so the names that hold the address
are defined after the allocation in the same iteration, and SSA redefines them in the next
iteration before any use; with `no_escape` nothing else holds it.
-/

namespace Escape

abbrev Name := Nat
abbrev Alloc := Nat

/-- a value: an opaque payload and its provenance -/
structure Val where
  bits : Nat
  prov : Alloc → Prop

inductive Instr where
  | alloc (d : Name) (a : Alloc)
  | derive (d : Name) (srcs : List Name)
  | load (d : Name) (addr : Name)
  | store (v : Name) (addr : Name)
  | pure (d : Name) (srcs : List Name)
  | call (d : Name) (args : List Name)
  | sink (v : Name)

structure St where
  env : Name → Val
  mem : Nat → Val
  /-- provenance received by sinks so far -/
  out : Alloc → Prop

/-- One step. Nondeterministic parts (bit payloads, the cell an address denotes, what a call
returns and writes) are free; provenance follows the table. `own` holds this frame's
allocation sites (a callee cannot create objects of them). -/
inductive Step (own : Alloc → Prop) : St → Instr → St → Prop
  | alloc (s : St) (d : Name) (a : Alloc) (bits : Nat) :
      Step own s (.alloc d a) { s with env := fun x => if x = d then ⟨bits, fun b => b = a⟩ else s.env x }
  | derive (s : St) (d : Name) (srcs : List Name) (v : Val)
      (h : ∀ b, v.prov b → ∃ x ∈ srcs, (s.env x).prov b) :
      Step own s (.derive d srcs) { s with env := fun x => if x = d then v else s.env x }
  | load (s : St) (d addr : Name) (cell : Nat) :
      Step own s (.load d addr) { s with env := fun x => if x = d then s.mem cell else s.env x }
  | store (s : St) (v addr : Name) (cell : Nat) :
      Step own s (.store v addr) { s with mem := fun c => if c = cell then s.env v else s.mem c }
  | pure (s : St) (d : Name) (srcs : List Name) (bits : Nat) :
      Step own s (.pure d srcs) { s with env := fun x => if x = d then ⟨bits, fun _ => False⟩ else s.env x }
  | call (s : St) (d : Name) (args : List Name) (r : Val) (mem' : Nat → Val)
      -- the summary contract: what the callee returns or leaves in memory comes from memory,
      -- or is an object the callee created (not of this frame's sites); never from the
      -- noescape arguments alone
      (hr : ∀ b, r.prov b → (∃ c, (s.mem c).prov b) ∨ ¬ own b)
      (hm : ∀ c b, (mem' c).prov b → (∃ c', (s.mem c').prov b) ∨ ¬ own b) :
      Step own s (.call d args) { s with env := fun x => if x = d then r else s.env x, mem := mem' }
  | sink (s : St) (v : Name) :
      Step own s (.sink v) { s with out := fun b => s.out b ∨ (s.env v).prov b }

inductive Run (own : Alloc → Prop) (prog : List Instr) : St → St → Prop
  | nil (s : St) : Run own prog s s
  | step (s s' s'' : St) (i : Instr) (hi : i ∈ prog) (h1 : Step own s i s') (h2 : Run own prog s' s'') :
      Run own prog s s''

/-- the analysis for allocation site a over the instructions of the function -/
structure NoEscape (prog : List Instr) (a : Alloc) (T : Name → Prop) : Prop where
  alloc_t : ∀ d, .alloc d a ∈ prog → T d
  derive_t : ∀ d srcs, .derive d srcs ∈ prog → (∃ x ∈ srcs, T x) → T d
  store_ok : ∀ v addr, .store v addr ∈ prog → ¬ T v
  sink_ok : ∀ v, .sink v ∈ prog → ¬ T v

def Inv (a : Alloc) (T : Name → Prop) (s : St) : Prop :=
  (∀ x, (s.env x).prov a → T x) ∧ (∀ c, ¬ (s.mem c).prov a) ∧ ¬ s.out a

theorem step_inv (own : Alloc → Prop) (prog : List Instr) (a : Alloc) (ha : own a) (T : Name → Prop)
    (h : NoEscape prog a T) (i : Instr) (hi : i ∈ prog) (s s' : St) (st : Step own s i s')
    (inv : Inv a T s) : Inv a T s' := by
  obtain ⟨he, hm, ho⟩ := inv
  cases st with
  | alloc d b bits =>
    refine ⟨fun x hx => ?_, hm, ho⟩
    by_cases e : x = d
    · subst e; simp only [ite_true] at hx; subst hx; exact h.alloc_t x hi
    · simp only [e, ite_false] at hx; exact he x hx
  | derive d srcs v hv =>
    refine ⟨fun x hx => ?_, hm, ho⟩
    by_cases e : x = d
    · subst e; simp only [ite_true] at hx
      obtain ⟨y, hy, hp⟩ := hv a hx
      exact h.derive_t x srcs hi ⟨y, hy, he y hp⟩
    · simp only [e, ite_false] at hx; exact he x hx
  | load d addr cell =>
    refine ⟨fun x hx => ?_, hm, ho⟩
    by_cases e : x = d
    · subst e; simp only [ite_true] at hx; exact absurd hx (hm cell)
    · simp only [e, ite_false] at hx; exact he x hx
  | store v addr cell =>
    refine ⟨he, fun c hc => ?_, ho⟩
    by_cases e : c = cell
    · simp only [e, ite_true] at hc; exact h.store_ok v addr hi (he v hc)
    · simp only [e, ite_false] at hc; exact hm c hc
  | pure d srcs bits =>
    refine ⟨fun x hx => ?_, hm, ho⟩
    by_cases e : x = d
    · subst e; simp at hx
    · simp only [e, ite_false] at hx; exact he x hx
  | call d args r mem' hr hmem =>
    refine ⟨fun x hx => ?_, fun c hc => ?_, ho⟩
    · by_cases e : x = d
      · subst e; simp only [ite_true] at hx
        rcases hr a hx with ⟨c, hc⟩ | hn
        · exact absurd hc (hm c)
        · exact absurd ha hn
      · simp only [e, ite_false] at hx; exact he x hx
    · rcases hmem c a hc with ⟨c', hc'⟩ | hn
      · exact hm c' hc'
      · exact hn ha
  | sink v =>
    refine ⟨he, hm, fun ho' => ?_⟩
    rcases ho' with ho' | ho'
    · exact ho ho'
    · exact h.sink_ok v hi (he v ho')

/-- go-escape is sound on the model: from a state where nothing holds the allocation's
address (before it runs), every execution keeps the address out of memory and out of every
sink, and only tainted names hold it. -/
theorem no_escape (own : Alloc → Prop) (prog : List Instr) (a : Alloc) (ha : own a) (T : Name → Prop)
    (h : NoEscape prog a T) (s s' : St) (r : Run own prog s s') (inv : Inv a T s) : Inv a T s' := by
  induction r with
  | nil => exact inv
  | step s s' s'' i hi h1 _ ih => exact ih (step_inv own prog a ha T h i hi s s' h1 inv)

/-- No sink ever receives the allocation's address. (The cycle rule, one stack slot reused
by every iteration, is not proved here: it rests on SSA dominance, see the file header.) -/
theorem out_never (own : Alloc → Prop) (prog : List Instr) (a : Alloc) (ha : own a) (T : Name → Prop)
    (h : NoEscape prog a T) (s s' : St) (r : Run own prog s s') (inv : Inv a T s) : ¬ s'.out a :=
  (no_escape own prog a ha T h s s' r inv).2.2

/-- the names an instruction reads -/
def reads : Instr → List Name
  | .alloc _ _ => []
  | .derive _ srcs => srcs
  | .load _ addr => [addr]
  | .store v addr => [v, addr]
  | .pure _ srcs => srcs
  | .call _ args => args
  | .sink v => [v]

/-- In-place reuse (reuse.go, as corrected after verify/testdata/progs/reusealias.go): the
chain values of an append chain are the taint set T of its backing array a (element
accesses only load and store through addresses, slices are never stored or passed on). If
after the freeing append no instruction reads a name of T (no chain value is used after the
free on any path), then no instruction after the free reads a value that can point to the
freed array, and memory holds none: the free is invisible. The run after the free starts
from a state where the invariant holds (by `no_escape` over the run up to the free). -/
theorem no_use_after_free (own : Alloc → Prop) (prog : List Instr) (a : Alloc) (ha : own a)
    (T : Name → Prop) (h : NoEscape prog a T) (s s' : St) (r : Run own prog s s') (inv : Inv a T s)
    (dead : ∀ i ∈ prog, ∀ x ∈ reads i, ¬ T x) :
    Inv a T s' ∧ ∀ i ∈ prog, ∀ x ∈ reads i, ¬ (s'.env x).prov a := by
  have hi := no_escape own prog a ha T h s s' r inv
  exact ⟨hi, fun i hi' x hx hp => dead i hi' x hx (hi.1 x hp)⟩

end Escape

#print axioms Escape.no_escape
#print axioms Escape.no_use_after_free
