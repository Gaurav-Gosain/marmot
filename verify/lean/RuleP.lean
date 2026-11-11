/-
RuleP.lean: Rule P of docs/loops.md (loop-parallelize), the independence part.

Model. Memory maps byte addresses to values. Iteration k of the loop is a list of steps over
the memory and the iteration's own local state, which starts as `loc0 k` (inductions are
recomputed from k in the chunk function, so an iteration starts from a state that depends
only on k: clause 2). A step reads memory only on its read set and writes only its write set
(the frame conditions below); steps are total (clause 1: no exit but the latch, so no panic;
clause 4: no calls with effects, no allocation, no I/O). Clause 5 is the disjointness
hypothesis: what one iteration writes, no other iteration reads or writes.

A schedule is any interleaving of the iterations' steps that runs every step of every
iteration once and keeps each iteration's own order (chunks on different threads, in any
order, interleaved at any granularity). Theorem `schedule_eq`: every schedule ends with the
same memory as the sequential loop. Reductions are in RuleR.lean.

Trusted: that the chunk functions really are this model of L (Parallelize.cpp's outlining),
and that clause 5 as checked by the pass (affine windows, alias analysis, run time range
checks, Rule P rows) gives the disjointness hypothesis. The second is the arithmetic of
`windows_disjoint` below for the same-base case.
-/

namespace RuleP

variable {V L : Type}

abbrev Mem (V : Type) := Nat → V

/-- f with the value at k replaced by v -/
def upd {α : Type} (f : Nat → α) (k : Nat) (v : α) : Nat → α := fun j => if j = k then v else f j

@[simp] theorem upd_self {α : Type} (f : Nat → α) (k : Nat) (v : α) : upd f k v k = v := by simp [upd]
theorem upd_ne {α : Type} (f : Nat → α) {k j : Nat} (v : α) (h : j ≠ k) : upd f k v j = f j := by
  simp [upd, h]

structure Step (L V : Type) where
  f : Mem V → L → Mem V × L
  rd : Nat → Prop
  wr : Nat → Prop
  /-- addresses outside the write set keep their value -/
  frame_wr : ∀ m l a, ¬ wr a → (f m l).1 a = m a
  /-- the result (new local state and written values) depends on memory only through the
  read set -/
  frame_rd : ∀ m m' l, (∀ a, rd a → m a = m' a) →
    (f m l).2 = (f m' l).2 ∧ ∀ a, wr a → (f m l).1 a = (f m' l).1 a

/-- the loop: n iterations, iteration k is `iter k` from local state `loc0 k` -/
structure Loop (L V : Type) where
  n : Nat
  iter : Nat → List (Step L V)
  loc0 : Nat → L

def Loop.R (P : Loop L V) (k a : Nat) : Prop := ∃ s ∈ P.iter k, s.rd a
def Loop.W (P : Loop L V) (k a : Nat) : Prop := ∃ s ∈ P.iter k, s.wr a

/-- clause 5: writes of one iteration are disjoint from all accesses of every other -/
def Loop.Disjoint (P : Loop L V) : Prop :=
  ∀ k k' a, k < P.n → k' < P.n → k ≠ k' → P.W k a → ¬ P.R k' a ∧ ¬ P.W k' a

/-- iteration k alone: its first j steps from memory m0 -/
def solo (P : Loop L V) (m0 : Mem V) (k : Nat) : Nat → Mem V × L
  | 0 => (m0, P.loc0 k)
  | j + 1 =>
    match (P.iter k)[j]? with
    | some s => s.f (solo P m0 k j).1 (solo P m0 k j).2
    | none => solo P m0 k j

structure State (L V : Type) where
  m : Mem V
  loc : Nat → L
  pc : Nat → Nat

/-- run the next step of iteration k (nothing if it has run all its steps) -/
def exec1 (P : Loop L V) (st : State L V) (k : Nat) : State L V :=
  match (P.iter k)[st.pc k]? with
  | some s =>
    let r := s.f st.m (st.loc k)
    { m := r.1, loc := upd st.loc k r.2, pc := upd st.pc k (st.pc k + 1) }
  | none => st

def run (P : Loop L V) (st : State L V) (sched : List Nat) : State L V := sched.foldl (exec1 P) st

def init (P : Loop L V) (m0 : Mem V) : State L V := { m := m0, loc := P.loc0, pc := fun _ => 0 }

/-- a schedule: only iterations of the loop, each exactly as often as it has steps -/
def Loop.Sched (P : Loop L V) (sched : List Nat) : Prop :=
  (∀ k ∈ sched, k < P.n) ∧ ∀ k < P.n, sched.count k = (P.iter k).length

/-- the sequential loop: all steps of iteration 0, then of 1, ... -/
def seqSched (P : Loop L V) : List Nat :=
  (List.range P.n).flatMap fun k => List.replicate (P.iter k).length k

theorem count_seq (P : Loop L V) (k m : Nat) (hk : k < m) :
    ((List.range m).flatMap fun j => List.replicate (P.iter j).length j).count k = (P.iter k).length := by
  induction m with
  | zero => omega
  | succ m ih =>
    rw [List.range_succ, List.flatMap_append, List.count_append]
    simp only [List.flatMap_cons, List.flatMap_nil, List.append_nil, List.count_replicate]
    by_cases e : k = m
    · subst e
      have : ((List.range k).flatMap fun j => List.replicate (P.iter j).length j).count k = 0 := by
        rw [List.count_eq_zero]
        simp only [List.mem_flatMap, List.mem_range, List.mem_replicate, not_exists, not_and]
        intro j hj _ hk; omega
      rw [this]; simp
    · rw [ih (by omega)]; simp [Ne.symm e, e]

theorem seq_is_sched (P : Loop L V) : P.Sched (seqSched P) := by
  constructor
  · intro k hk
    simp only [seqSched, List.mem_flatMap, List.mem_range, List.mem_replicate] at hk
    obtain ⟨k', hk', _, rfl⟩ := hk
    exact hk'
  · intro k hk
    exact count_seq P k P.n hk

/-! ## The invariant: every iteration sees its own footprint as if it ran alone -/

def Inv (P : Loop L V) (m0 : Mem V) (st : State L V) : Prop :=
  (∀ k < P.n, st.pc k ≤ (P.iter k).length ∧ st.loc k = (solo P m0 k (st.pc k)).2 ∧
      ∀ a, (P.R k a ∨ P.W k a) → st.m a = (solo P m0 k (st.pc k)).1 a) ∧
  (∀ a, (∀ k < P.n, ¬ P.W k a) → st.m a = m0 a)

theorem inv_init (P : Loop L V) (m0 : Mem V) : Inv P m0 (init P m0) := by
  refine ⟨fun k _ => ⟨Nat.zero_le _, rfl, fun a _ => rfl⟩, fun a _ => rfl⟩

/-- what iteration k's solo run leaves outside its write set -/
theorem solo_frame (P : Loop L V) (m0 : Mem V) (k j a : Nat) (h : ¬ P.W k a) :
    (solo P m0 k j).1 a = m0 a := by
  induction j with
  | zero => rfl
  | succ j ih =>
    simp only [solo]
    split
    · rename_i s hs
      have hw : ¬ s.wr a := fun hw => h ⟨s, List.mem_of_getElem? hs, hw⟩
      rw [s.frame_wr _ _ _ hw]; exact ih
    · exact ih

theorem inv_step (P : Loop L V) (m0 : Mem V) (hd : P.Disjoint) (st : State L V) (k : Nat)
    (hk : k < P.n) (h : Inv P m0 st) : Inv P m0 (exec1 P st k) := by
  obtain ⟨hi, ho⟩ := h
  unfold exec1
  split
  · rename_i s hs
    obtain ⟨hpc, hloc, hmem⟩ := hi k hk
    have hlt : st.pc k < (P.iter k).length := by
      rcases Nat.lt_or_ge (st.pc k) (P.iter k).length with h' | h'
      · exact h'
      · rw [List.getElem?_eq_none h'] at hs; cases hs
    have smem : s ∈ P.iter k := List.mem_of_getElem? hs
    -- the step sees the same read set as in the solo run
    have agree : ∀ a, s.rd a → st.m a = (solo P m0 k (st.pc k)).1 a :=
      fun a ha => hmem a (Or.inl ⟨s, smem, ha⟩)
    obtain ⟨floc, fwr⟩ := s.frame_rd _ _ (st.loc k) agree
    have solo_next : solo P m0 k (st.pc k + 1) = s.f (solo P m0 k (st.pc k)).1 (solo P m0 k (st.pc k)).2 := by
      simp only [solo, hs]
    refine ⟨fun k' hk' => ?_, fun a ha => ?_⟩
    · by_cases e : k' = k
      · subst e
        simp only [upd_self]
        refine ⟨hlt, ?_, fun a ha => ?_⟩
        · rw [solo_next, ← hloc]; exact floc
        · rw [solo_next, ← hloc]
          by_cases hw : s.wr a
          · exact fwr a hw
          · rw [s.frame_wr _ _ _ hw, s.frame_wr _ _ _ hw]; exact hmem a ha
      · simp only [upd_ne _ _ e]
        obtain ⟨hpc', hloc', hmem'⟩ := hi k' hk'
        refine ⟨hpc', hloc', fun a ha => ?_⟩
        have hw : ¬ s.wr a := by
          intro hw
          have := hd k k' a hk hk' (Ne.symm e) ⟨s, smem, hw⟩
          rcases ha with ha | ha
          · exact this.1 ha
          · exact this.2 ha
        rw [s.frame_wr _ _ _ hw]; exact hmem' a ha
    · have hw : ¬ s.wr a := fun hw => ha k hk ⟨s, smem, hw⟩
      simp only
      rw [s.frame_wr _ _ _ hw]; exact ho a ha
  · exact ⟨hi, ho⟩

theorem inv_run (P : Loop L V) (m0 : Mem V) (hd : P.Disjoint) (sched : List Nat)
    (hs : ∀ k ∈ sched, k < P.n) (st : State L V) (h : Inv P m0 st) : Inv P m0 (run P st sched) := by
  induction sched generalizing st with
  | nil => exact h
  | cons k r ih =>
    simp only [run, List.foldl_cons]
    exact ih (fun k' hk' => hs k' (List.mem_cons_of_mem _ hk'))
      _ (inv_step P m0 hd st k (hs k List.mem_cons_self) h)

/-- every scheduled step of k advances k's counter, up to its length -/
theorem pc_run (P : Loop L V) (sched : List Nat) (st : State L V) (k : Nat)
    (h : st.pc k + sched.count k ≤ (P.iter k).length) :
    (run P st sched).pc k = st.pc k + sched.count k := by
  induction sched generalizing st with
  | nil => simp [run]
  | cons k' r ih =>
    simp only [run, List.foldl_cons] at *
    by_cases e : k' = k
    · subst e
      simp only [List.count_cons_self] at h ⊢
      have hlt : st.pc k' < (P.iter k').length := by omega
      have hs : ∃ s, (P.iter k')[st.pc k']? = some s := ⟨_, List.getElem?_eq_getElem hlt⟩
      obtain ⟨s, hs⟩ := hs
      have step : (exec1 P st k').pc k' = st.pc k' + 1 := by
        simp only [exec1, hs, upd_self]
      have := ih (exec1 P st k') (by rw [step]; omega)
      rw [this, step]; omega
    · have hc : (k' :: r).count k = r.count k := by
        simp [List.count_cons, e]
      rw [hc] at h ⊢
      have step : (exec1 P st k').pc k = st.pc k := by
        unfold exec1; split
        · simp only [upd_ne _ _ (Ne.symm e)]
        · rfl
      have := ih (exec1 P st k') (by rw [step]; omega)
      rw [this, step]

/-- After a full schedule the memory is determined: an address written by iteration k holds
what k's solo run left there, any other address its initial value. -/
theorem final_mem (P : Loop L V) (m0 : Mem V) (hd : P.Disjoint) (sched : List Nat)
    (hs : P.Sched sched) (a : Nat) :
    ((∃ k < P.n, P.W k a) → ∀ k < P.n, P.W k a →
        (run P (init P m0) sched).m a = (solo P m0 k (P.iter k).length).1 a) ∧
    ((∀ k < P.n, ¬ P.W k a) → (run P (init P m0) sched).m a = m0 a) := by
  have hinv := inv_run P m0 hd sched hs.1 (init P m0) (inv_init P m0)
  obtain ⟨hi, ho⟩ := hinv
  refine ⟨fun _ k hk hw => ?_, fun h => ho a h⟩
  have hpc : (run P (init P m0) sched).pc k = (P.iter k).length := by
    rw [pc_run P sched (init P m0) k (by simp [init, hs.2 k hk])]
    simp [init, hs.2 k hk]
  obtain ⟨_, _, hm⟩ := hi k hk
  rw [hm a (Or.inr hw), hpc]

/-- Rule P: every schedule ends with the memory of the sequential loop. -/
theorem schedule_eq (P : Loop L V) (m0 : Mem V) (hd : P.Disjoint) (sched : List Nat)
    (hs : P.Sched sched) :
    (run P (init P m0) sched).m = (run P (init P m0) (seqSched P)).m := by
  funext a
  have f1 := final_mem P m0 hd sched hs a
  have f2 := final_mem P m0 hd (seqSched P) (seq_is_sched P) a
  by_cases h : ∃ k < P.n, P.W k a
  · obtain ⟨k, hk, hw⟩ := h
    rw [f1.1 ⟨k, hk, hw⟩ k hk hw, f2.1 ⟨k, hk, hw⟩ k hk hw]
  · have h' : ∀ k < P.n, ¬ P.W k a := fun k hk hw => h ⟨k, hk, hw⟩
    rw [f1.2 h', f2.2 h']

/-! ## Clause 5, same base: disjoint windows

For one base pointer, iteration k accesses bytes [coef*k + lo, coef*k + hi + size) (the
offsets of all accesses to the base over the inner loops, Rule P condition 5). If for every
store X and every access Y, hi_X + size_X - lo_Y <= |coef| and hi_Y + size_Y - lo_X <= |coef|,
then the bytes a store of iteration k touches and the bytes any access of another iteration
k' touches are disjoint. -/

/-- an access window: offsets [lo, hi] and access size -/
structure Win where
  lo : Int
  hi : Int
  size : Int

/-- byte a is touched by window w in iteration k -/
def Win.touches (coef : Int) (w : Win) (k : Int) (a : Int) : Prop :=
  coef * k + w.lo ≤ a ∧ a < coef * k + w.hi + w.size

/-- two different iterations are at least |coef| bytes apart -/
theorem apart (coef k k' : Int) (hne : k ≠ k') :
    (coef.natAbs : Int) ≤ coef * k - coef * k' ∨ coef * k - coef * k' ≤ -(coef.natAbs : Int) := by
  have hd : coef * k - coef * k' = coef * (k - k') := by rw [Int.mul_sub]
  rw [hd]
  rcases Int.le_total 0 coef with hc | hc
  · have ha : (coef.natAbs : Int) = coef := Int.natAbs_of_nonneg hc
    rcases Int.lt_or_gt_of_ne hne with h | h
    · have : coef * (k' - k) ≥ coef * 1 := Int.mul_le_mul_of_nonneg_left (by omega) hc
      have e : coef * (k - k') = -(coef * (k' - k)) := by rw [← Int.mul_neg, Int.neg_sub]
      omega
    · have : coef * (k - k') ≥ coef * 1 := Int.mul_le_mul_of_nonneg_left (by omega) hc
      omega
  · have ha : (coef.natAbs : Int) = -coef := by omega
    rcases Int.lt_or_gt_of_ne hne with h | h
    · have : coef * (k' - k) ≤ coef * 1 := Int.mul_le_mul_of_nonpos_left hc (by omega)
      have e : coef * (k - k') = -(coef * (k' - k)) := by rw [← Int.mul_neg, Int.neg_sub]
      omega
    · have : coef * (k - k') ≤ coef * 1 := Int.mul_le_mul_of_nonpos_left hc (by omega)
      omega

theorem windows_disjoint (coef : Int) (x y : Win) (k k' : Int) (hne : k ≠ k')
    (h1 : x.hi + x.size - y.lo ≤ coef.natAbs) (h2 : y.hi + y.size - x.lo ≤ coef.natAbs)
    (a : Int) (hx : x.touches coef k a) (hy : y.touches coef k' a) : False := by
  unfold Win.touches at hx hy
  have := apart coef k k' hne
  omega

/-- The strided rule (column accesses, the transpose case): iteration k touches the bytes
c*k + t*j + [0, size) for rows j. With c >= size (columns of one row do not overlap) and
c*K + size <= t (all columns of iterations 0..K fit in one row stride), two different
iterations k, k' in [0, K] never touch the same byte. -/
theorem strided_disjoint (c t size K k k' j j' u u' : Int) (hs : 0 < size) (hc : size ≤ c)
    (ht : c * K + size ≤ t) (hk : 0 ≤ k ∧ k ≤ K) (hk' : 0 ≤ k' ∧ k' ≤ K)
    (hu : 0 ≤ u ∧ u < size) (hu' : 0 ≤ u' ∧ u' < size)
    (heq : c * k + t * j + u = c * k' + t * j' + u') : k = k' := by
  have hc0 : 0 < c := by omega
  -- |c*(k - k')| <= c*K
  have hkk : c * k ≤ c * K := Int.mul_le_mul_of_nonneg_left hk.2 (by omega)
  have hkk' : c * k' ≤ c * K := Int.mul_le_mul_of_nonneg_left hk'.2 (by omega)
  have h0 : 0 ≤ c * k := Int.mul_nonneg (by omega) hk.1
  have h0' : 0 ≤ c * k' := Int.mul_nonneg (by omega) hk'.1
  -- first the rows agree: otherwise t*|j - j'| >= t > the column spread
  have hj : j = j' := by
    rcases Int.lt_trichotomy j j' with h | h | h
    · have : t * j + t ≤ t * j' := by
        have := Int.mul_le_mul_of_nonneg_left (show j + 1 ≤ j' by omega) (show 0 ≤ t by omega)
        rw [Int.mul_add, Int.mul_one] at this; exact this
      omega
    · exact h
    · have : t * j' + t ≤ t * j := by
        have := Int.mul_le_mul_of_nonneg_left (show j' + 1 ≤ j by omega) (show 0 ≤ t by omega)
        rw [Int.mul_add, Int.mul_one] at this; exact this
      omega
  subst hj
  -- then c*(k - k') = u' - u with |u' - u| < size <= c
  rcases Int.lt_trichotomy k k' with h | h | h
  · have : c * k + c ≤ c * k' := by
      have := Int.mul_le_mul_of_nonneg_left (show k + 1 ≤ k' by omega) (show 0 ≤ c by omega)
      rw [Int.mul_add, Int.mul_one] at this; exact this
    omega
  · exact h
  · have : c * k' + c ≤ c * k := by
      have := Int.mul_le_mul_of_nonneg_left (show k' + 1 ≤ k by omega) (show 0 ≤ c by omega)
      rw [Int.mul_add, Int.mul_one] at this; exact this
    omega

end RuleP

#print axioms RuleP.schedule_eq
#print axioms RuleP.windows_disjoint
#print axioms RuleP.strided_disjoint
