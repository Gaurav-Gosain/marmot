/-
Region.lean: region allocation (docs/frontend-passes.md, "Regions"; region.go).

Model. A pointer value points into one object or is not a pointer. Objects allocated by the
region clone belong to the region R. Instructions of the call's block (with the cloned
callee inlined in the model, which is what the clone's allocations amount to):
  ralloc d a          d := pointer to a new object a of the region (a ∈ R)
  alloc d a           d := pointer to a new object a outside the region
  derive d x          d := a pointer into the same object as x (field or element address,
                      phi, extract), or x's value
  load d addr         d := the memory cell addr points to
  store v addr        the cell addr points to := v
  pure d xs           compare, or a call whose result holds no pointers: d is not a pointer
  ncall xs            call to a function whose parameters do not escape: by its contract it
                      neither keeps nor writes the arguments' region pointers
  sink v              anything else (return from the caller, store into a global, ...)
Each cell belongs to the object it is part of; a store through addr writes a cell of the
object addr points to.

The analysis gives two sets of names. C ("may hold a region pointer") contains every ralloc
result and is closed under derive and under loads (pointers loaded out of the graph are part
of it). M ("surely points into a region object") contains every ralloc result and is closed
under derive from M. The rule: a name of C is only stored into a cell addressed by a name of
M ("stored into fields of such objects"), never sunk, and passed only to ncall.

Holders. H is the set of objects allowed to hold region pointers: the region objects (R ⊆ H),
and for frame regions (frameregion.go) also the caller's frame object, which does not escape
even deeply and dies at the return that frees the region. M ("surely points into a holder")
contains region allocations, the frame object's address (the parameter), and addresses
derived from them; never a value loaded from memory: a loaded pointer may be a heap pointer
the caller stored there (counterexamples verify/testdata/progs/regionstore.go and
frameregion.go are exactly that mistake).

Theorem `confined`: from the initial state of `init_inv`, in every execution, (1) every name holding a region pointer is in C,
(2) every memory cell holding a region pointer belongs to a holder (a region object or the
frame object), (3) no sink
receives a region pointer. So when the last use of the names in C is behind (the pass frees
the region after the last use in the block), nothing outside the region points into it.
Trusted: the ncall contract, and that the pass's last-use computation is right (SSA: names of
C are not used after the free).
-/

namespace Region

abbrev Name := Nat
abbrev Obj := Nat

structure Val where
  ptr : Option Obj

inductive Instr where
  | ralloc (d : Name) (a : Obj)
  | alloc (d : Name) (a : Obj)
  | derive (d x : Name)
  | load (d addr : Name)
  | store (v addr : Name)
  | pure (d : Name)
  | ncall (xs : List Name)
  | sink (v : Name)

structure St where
  env : Name → Val
  /-- memory: (object, cell index) -> value -/
  mem : Obj × Nat → Val
  out : Obj → Prop

def upd (s : St) (d : Name) (v : Val) : St := { s with env := fun x => if x = d then v else s.env x }

inductive Step (R : Obj → Prop) : St → Instr → St → Prop
  | ralloc (s : St) (d : Name) (a : Obj) (ha : R a) : Step R s (.ralloc d a) (upd s d ⟨some a⟩)
  | alloc (s : St) (d : Name) (a : Obj) (ha : ¬ R a) : Step R s (.alloc d a) (upd s d ⟨some a⟩)
  | derive (s : St) (d x : Name) : Step R s (.derive d x) (upd s d (s.env x))
  | load (s : St) (d addr : Name) (o : Obj) (i : Nat) (ho : (s.env addr).ptr = some o) :
      Step R s (.load d addr) (upd s d (s.mem (o, i)))
  | store (s : St) (v addr : Name) (o : Obj) (i : Nat) (ho : (s.env addr).ptr = some o) :
      Step R s (.store v addr) { s with mem := fun c => if c = (o, i) then s.env v else s.mem c }
  | pure (s : St) (d : Name) : Step R s (.pure d) (upd s d ⟨none⟩)
  /-- the contract: the callee writes no region pointer into memory outside the region -/
  | ncall (s : St) (xs : List Name) (mem' : Obj × Nat → Val)
      (hm : ∀ c o, (mem' c).ptr = some o → R o → (s.mem c).ptr = some o ∨ R c.1) :
      Step R s (.ncall xs) { s with mem := mem' }
  | sink (s : St) (v : Name) :
      Step R s (.sink v) { s with out := fun o => s.out o ∨ (s.env v).ptr = some o }

inductive Run (R : Obj → Prop) (prog : List Instr) : St → St → Prop
  | nil (s : St) : Run R prog s s
  | step (s s' s'' : St) (i : Instr) (hi : i ∈ prog) (h1 : Step R s i s') (h2 : Run R prog s' s'') :
      Run R prog s s''

structure Rule (prog : List Instr) (C M : Name → Prop) : Prop where
  ralloc_c : ∀ d a, .ralloc d a ∈ prog → C d
  ralloc_m : ∀ d a, .ralloc d a ∈ prog → M d
  derive_c : ∀ d x, .derive d x ∈ prog → C x → C d
  derive_m : ∀ d x, .derive d x ∈ prog → M x → M d
  load_c : ∀ d addr, .load d addr ∈ prog → C d
  /-- M names are only defined by ralloc and derive from M -/
  m_def : ∀ d, M d → ∀ i ∈ prog, match i with
      | .alloc d' _ => d' ≠ d
      | .load d' _ => d' ≠ d
      | .pure d' => d' ≠ d
      | .derive d' x => d' = d → M x
      | _ => True
  store_ok : ∀ v addr, .store v addr ∈ prog → C v → M addr
  sink_ok : ∀ v, .sink v ∈ prog → ¬ C v

def Inv (R H : Obj → Prop) (C M : Name → Prop) (s : St) : Prop :=
  (∀ x o, (s.env x).ptr = some o → R o → C x) ∧
  (∀ x, M x → (s.env x).ptr = none ∨ ∃ o, (s.env x).ptr = some o ∧ H o) ∧
  (∀ c o, (s.mem c).ptr = some o → R o → H c.1) ∧
  (∀ o, s.out o → ¬ R o)

theorem step_inv (R H : Obj → Prop) (hRH : ∀ o, R o → H o) (prog : List Instr) (C M : Name → Prop)
    (h : Rule prog C M) (i : Instr) (hi : i ∈ prog) (s s' : St) (st : Step R s i s')
    (inv : Inv R H C M s) : Inv R H C M s' := by
  obtain ⟨he, hm, hmem, ho⟩ := inv
  cases st with
  | ralloc d a ha =>
    refine ⟨fun x o hx hr => ?_, fun x hx => ?_, hmem, ho⟩
    · by_cases e : x = d
      · subst e; exact h.ralloc_c x a hi
      · simp only [upd, e, ite_false] at hx; exact he x o hx hr
    · by_cases e : x = d
      · subst e; exact Or.inr ⟨a, by simp [upd], hRH a ha⟩
      · simp only [upd, e, ite_false]; exact hm x hx
  | alloc d a ha =>
    refine ⟨fun x o hx hr => ?_, fun x hx => ?_, hmem, ho⟩
    · by_cases e : x = d
      · subst e; simp only [upd, ite_true, Option.some.injEq] at hx; subst hx; exact absurd hr ha
      · simp only [upd, e, ite_false] at hx; exact he x o hx hr
    · by_cases e : x = d
      · subst e; have := h.m_def x hx _ hi; simp at this
      · simp only [upd, e, ite_false]; exact hm x hx
  | derive d x =>
    refine ⟨fun y o hy hr => ?_, fun y hy => ?_, hmem, ho⟩
    · by_cases e : y = d
      · subst e; simp only [upd, ite_true] at hy; exact h.derive_c y x hi (he x o hy hr)
      · simp only [upd, e, ite_false] at hy; exact he y o hy hr
    · by_cases e : y = d
      · subst e; have hx : M x := by have := h.m_def y hy _ hi; simpa using this
        simp only [upd, ite_true]; exact hm x hx
      · simp only [upd, e, ite_false]; exact hm y hy
  | load d addr o i ho' =>
    refine ⟨fun y o' hy hr => ?_, fun y hy => ?_, hmem, ho⟩
    · by_cases e : y = d
      · subst e; exact h.load_c y addr hi
      · simp only [upd, e, ite_false] at hy; exact he y o' hy hr
    · by_cases e : y = d
      · subst e; have := h.m_def y hy _ hi; simp at this
      · simp only [upd, e, ite_false]; exact hm y hy
  | store v addr o i ho' =>
    refine ⟨he, hm, fun c o' hc hr => ?_, ho⟩
    by_cases e : c = (o, i)
    · subst e; simp only [ite_true] at hc
      rcases hm addr (h.store_ok v addr hi (he v o' hc hr)) with h2 | ⟨o2, h2, hr2⟩
      · rw [ho'] at h2; cases h2
      · rw [ho'] at h2; cases h2; exact hr2
    · simp only [e, ite_false] at hc; exact hmem c o' hc hr
  | pure d =>
    refine ⟨fun y o hy hr => ?_, fun y hy => ?_, hmem, ho⟩
    · by_cases e : y = d
      · subst e; simp [upd] at hy
      · simp only [upd, e, ite_false] at hy; exact he y o hy hr
    · by_cases e : y = d
      · subst e; have := h.m_def y hy _ hi; simp at this
      · simp only [upd, e, ite_false]; exact hm y hy
  | ncall xs mem' hm' =>
    refine ⟨he, hm, fun c o hc hr => ?_, ho⟩
    rcases hm' c o hc hr with h1 | h1
    · exact hmem c o h1 hr
    · exact hRH _ h1
  | sink v =>
    refine ⟨he, hm, hmem, fun o ho' => ?_⟩
    rcases ho' with ho' | ho'
    · exact ho o ho'
    · intro hr; exact h.sink_ok v hi (he v o ho' hr)

theorem confined (R H : Obj → Prop) (hRH : ∀ o, R o → H o) (prog : List Instr) (C M : Name → Prop)
    (h : Rule prog C M) (s s' : St) (r : Run R prog s s') (inv : Inv R H C M s) : Inv R H C M s' := by
  induction r with
  | nil => exact inv
  | step s s' s'' i hi h1 _ ih => exact ih (step_inv R H hRH prog C M h i hi s s' h1 inv)

/-- the invariant holds before the block runs: no name holds a pointer yet, memory holds no
region pointer (the region is new), nothing was sunk -/
theorem init_inv (R H : Obj → Prop) (C M : Name → Prop) (mem : Obj × Nat → Val)
    (hmem : ∀ c o, (mem c).ptr = some o → ¬ R o) :
    Inv R H C M { env := fun _ => ⟨none⟩, mem := mem, out := fun _ => False } :=
  ⟨fun _ _ h => by simp at h, fun _ _ => Or.inl rfl, fun c o h hr => absurd hr (hmem c o h),
    fun _ h => h.elim⟩

/-- Frame regions: the parameter holds the frame object's address before the callee runs
(a holder), memory holds no region pointer yet. -/
theorem init_frame (R H : Obj → Prop) (C M : Name → Prop) (mem : Obj × Nat → Val) (param : Name)
    (A : Obj) (hA : H A) (hAR : ¬ R A) (hmem : ∀ c o, (mem c).ptr = some o → ¬ R o) :
    Inv R H C M { env := fun x => if x = param then ⟨some A⟩ else ⟨none⟩, mem := mem, out := fun _ => False } := by
  refine ⟨fun x o hx hr => ?_, fun x _ => ?_, fun c o h hr => absurd hr (hmem c o h), fun _ h => h.elim⟩
  · by_cases e : x = param
    · simp only [e, ite_true, Option.some.injEq] at hx; subst hx; exact absurd hr hAR
    · simp [e] at hx
  · by_cases e : x = param
    · simp only [e, ite_true]; exact Or.inr ⟨A, rfl, hA⟩
    · simp [e]

/-! ## No-scan regions (frame regions, frameregion.go)

A frame region is allocated noscan: the collector never reads its cells. A heap pointer stored
in a region object is therefore invisible, and the heap object can be freed while the region
still points to it (counterexample verify/testdata/progs/frameregionwrite.go: a callee with a
non-escaping parameter stores a fresh heap pointer into the region object it was given).
Invariant NS: every pointer cell of a region object points to a holder (a region object or
the frame object, both alive while the region is). Rule: a store through an address that may
point into the region (a name of C) stores a value of M (surely points to a holder) or of Z
(names defined only by `pure`, never a pointer). Call contract: a call writes into region
cells only what was there or a pointer to a holder (ncallOK). -/

def NS (R H : Obj → Prop) (Z : Name → Prop) (s : St) : Prop :=
  (∀ x, Z x → (s.env x).ptr = none) ∧
  (∀ c o, R c.1 → (s.mem c).ptr = some o → H o)

structure RuleNS (prog : List Instr) (C M Z : Name → Prop) : Prop where
  /-- Z names are only defined by pure -/
  z_def : ∀ d, Z d → ∀ i ∈ prog, match i with
      | .ralloc d' _ => d' ≠ d
      | .alloc d' _ => d' ≠ d
      | .derive d' _ => d' ≠ d
      | .load d' _ => d' ≠ d
      | _ => True
  store_ns : ∀ v addr, .store v addr ∈ prog → C addr → M v ∨ Z v

/-- the call contract for a noscan region -/
def ncallOK (R H : Obj → Prop) (s s' : St) : Prop :=
  ∀ c o, R c.1 → (s'.mem c).ptr = some o → (s.mem c).ptr = some o ∨ H o

theorem step_ns (R H : Obj → Prop) (prog : List Instr) (C M Z : Name → Prop)
    (_h : Rule prog C M) (hz : RuleNS prog C M Z) (i : Instr) (hi : i ∈ prog) (s s' : St)
    (st : Step R s i s') (hns : ∀ xs, i = .ncall xs → ncallOK R H s s')
    (inv : Inv R H C M s) (ns : NS R H Z s) : NS R H Z s' := by
  obtain ⟨he, hm, _, _⟩ := inv
  obtain ⟨hzn, hmem⟩ := ns
  cases st with
  | ralloc d a ha =>
    refine ⟨fun x hx => ?_, hmem⟩
    by_cases e : x = d
    · subst e; have := hz.z_def x hx _ hi; simp at this
    · simp only [upd, e, ite_false]; exact hzn x hx
  | alloc d a ha =>
    refine ⟨fun x hx => ?_, hmem⟩
    by_cases e : x = d
    · subst e; have := hz.z_def x hx _ hi; simp at this
    · simp only [upd, e, ite_false]; exact hzn x hx
  | derive d x =>
    refine ⟨fun y hy => ?_, hmem⟩
    by_cases e : y = d
    · subst e; have := hz.z_def y hy _ hi; simp at this
    · simp only [upd, e, ite_false]; exact hzn y hy
  | load d addr o i ho' =>
    refine ⟨fun y hy => ?_, hmem⟩
    by_cases e : y = d
    · subst e; have := hz.z_def y hy _ hi; simp at this
    · simp only [upd, e, ite_false]; exact hzn y hy
  | store v addr o i ho' =>
    refine ⟨hzn, fun c o' hr hc => ?_⟩
    by_cases e : c = (o, i)
    · subst e
      simp only [ite_true] at hc
      have hC : C addr := he addr o ho' hr
      rcases hz.store_ns v addr hi hC with hv | hv
      · rcases hm v hv with h2 | ⟨o2, h2, hh⟩
        · rw [h2] at hc; cases hc
        · rw [h2] at hc; cases hc; exact hh
      · rw [hzn v hv] at hc; cases hc
    · simp only [e, ite_false] at hc; exact hmem c o' hr hc
  | pure d =>
    refine ⟨fun y hy => ?_, hmem⟩
    by_cases e : y = d
    · subst e; simp [upd]
    · simp only [upd, e, ite_false]; exact hzn y hy
  | ncall xs mem' hm' =>
    refine ⟨hzn, fun c o hr hc => ?_⟩
    rcases hns xs rfl c o hr hc with h1 | h1
    · exact hmem c o hr h1
    · exact h1
  | sink v => exact ⟨hzn, hmem⟩

/-- executions whose calls keep the noscan contract -/
inductive RunNS (R H : Obj → Prop) (prog : List Instr) : St → St → Prop
  | nil (s : St) : RunNS R H prog s s
  | step (s s' s'' : St) (i : Instr) (hi : i ∈ prog) (h1 : Step R s i s')
      (hns : ∀ xs, i = .ncall xs → ncallOK R H s s') (h2 : RunNS R H prog s' s'') :
      RunNS R H prog s s''

/-- Theorem `confined_ns`: under both rules and the noscan call contract, the region stays
confined (as `confined`) and no region cell ever holds a pointer to anything but a holder:
the collector, which does not scan the region, loses no object the region refers to. -/
theorem confined_ns (R H : Obj → Prop) (hRH : ∀ o, R o → H o) (prog : List Instr)
    (C M Z : Name → Prop) (h : Rule prog C M) (hz : RuleNS prog C M Z) (s s' : St)
    (r : RunNS R H prog s s') (inv : Inv R H C M s) (ns : NS R H Z s) :
    Inv R H C M s' ∧ NS R H Z s' := by
  induction r with
  | nil => exact ⟨inv, ns⟩
  | step s s' s'' i hi h1 hns _ ih =>
    exact ih (step_inv R H hRH prog C M h i hi s s' h1 inv)
      (step_ns R H prog C M Z h hz i hi s s' h1 hns inv ns)

/-- NS holds at the start when the region's objects are fresh (zeroed) and the Z names hold
no pointer (no name holds one before the block runs) -/
theorem init_ns (R H : Obj → Prop) (Z : Name → Prop) (env : Name → Val) (mem : Obj × Nat → Val)
    (hz : ∀ x, Z x → (env x).ptr = none) (hfresh : ∀ c, R c.1 → (mem c).ptr = none) :
    NS R H Z { env := env, mem := mem, out := fun _ => False } :=
  ⟨hz, fun c o hr hc => by rw [hfresh c hr] at hc; cases hc⟩

/-- Not vacuous: the counterexample's pattern (a fresh heap object stored into a region object)
reaches a state where NS fails, and no RuleNS admits that program. Region R = {0}, holders
H = {0}, heap object 1. -/
example : ∃ s s', Step (· = 0) s (.store 2 1) s' ∧ NS (· = 0) (· = 0) (fun _ => False) s ∧
    ¬ NS (· = 0) (· = 0) (fun _ => False) s' := by
  let s : St := { env := fun x => if x = 1 then ⟨some 0⟩ else if x = 2 then ⟨some 1⟩ else ⟨none⟩,
                  mem := fun _ => ⟨none⟩, out := fun _ => False }
  refine ⟨s, _, Step.store s 2 1 0 0 (by simp [s]), ⟨fun _ h => h.elim, fun c o _ h => by simp [s] at h⟩, ?_⟩
  intro ⟨_, h⟩
  have := h (0, 0) 1 rfl (by simp [s])
  simp at this

example (C M Z : Name → Prop) (hC : C 1) (hM : ¬ M 2) :
    ¬ RuleNS [.ralloc 1 0, .alloc 2 1, .store 2 1] C M Z := by
  intro r
  rcases r.store_ns 2 1 (by simp) hC with h | h
  · exact hM h
  · have := r.z_def 2 h (.alloc 2 1) (by simp); simp at this

end Region

#print axioms Region.confined
#print axioms Region.init_inv
#print axioms Region.init_frame
#print axioms Region.confined_ns
#print axioms Region.init_ns
