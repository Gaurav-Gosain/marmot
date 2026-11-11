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

end Region

#print axioms Region.confined
#print axioms Region.init_inv
#print axioms Region.init_frame
