/-
Defer.lean: open-coded defers (docs/frontend-passes.md, "Open-coded defers"; opendefer.go).

1. `path_increasing`: the defer statements are numbered by a topological order of the acyclic
   defer region (reverse postorder is one: every edge goes to a higher number). Along any
   path the defer statements that run have strictly increasing numbers, so "a higher index
   ran later", and running the pending calls in descending index is LIFO order.

2. `runner_order`: the frame record holds a pending bit per defer statement. One invocation
   of the runner runs the highest pending call after clearing its bit. A panic or Goexit in a
   deferred call re-enters the runner later, so the runner's work is any sequence of single
   steps, interrupted anywhere. Whatever the interruptions, the calls made are exactly the
   pending ones, each once, in descending index.

Trusted: that opendefer.go numbers in reverse postorder of an acyclic region (the pass's
precondition: no defer in a loop), and that the runtime invokes the runner on panic and
Goexit while the record is on the defer chain.
-/

namespace Defer

/-! ## 1. Topological numbering -/

/-- a path b0 -> b1 -> ... in a graph with edge relation E -/
def IsPath (E : Nat → Nat → Prop) : List Nat → Prop
  | [] => True
  | [_] => True
  | a :: b :: r => E a b ∧ IsPath E (b :: r)

/-- the numbers along the path are strictly increasing -/
def Increasing (num : Nat → Nat) : List Nat → Prop
  | [] => True
  | [_] => True
  | a :: b :: r => num a < num b ∧ Increasing num (b :: r)

theorem path_increasing (E : Nat → Nat → Prop) (num : Nat → Nat)
    (topo : ∀ a b, E a b → num a < num b) (p : List Nat) (hp : IsPath E p) : Increasing num p := by
  induction p with
  | nil => trivial
  | cons a r ih =>
    cases r with
    | nil => trivial
    | cons b r' =>
      obtain ⟨hab, hr⟩ := hp
      exact ⟨topo a b hab, ih hr⟩

/-! ## 2. The runner -/

/-- pending indices below n, highest first -/
def pendingDesc (p : Nat → Bool) (n : Nat) : List Nat := (List.range n).reverse.filter p

def clear (p : Nat → Bool) (i : Nat) : Nat → Bool := fun j => if j = i then false else p j

/-- one runner step: clear the highest pending bit, then make that call (appended to the
trace of calls made) -/
def step (n : Nat) (st : (Nat → Bool) × List Nat) : (Nat → Bool) × List Nat :=
  match pendingDesc st.1 n with
  | [] => st
  | i :: _ => (clear st.1 i, st.2 ++ [i])

def steps (n : Nat) : Nat → (Nat → Bool) × List Nat → (Nat → Bool) × List Nat
  | 0, st => st
  | k + 1, st => steps n k (step n st)

theorem filter_clear_head (l : List Nat) (p : Nat → Bool) (i : Nat) (nd : (i :: l).Nodup)
    (h : l.filter p = l.filter p) : l.filter (clear p i) = l.filter p := by
  apply List.filter_congr
  intro x hx
  have : x ≠ i := by
    intro e; subst e; exact (List.nodup_cons.mp nd).1 hx
  simp [clear, this]

theorem range_rev_nodup (n : Nat) : ((List.range n).reverse).Nodup :=
  List.pairwise_reverse.mpr (List.nodup_range.imp (fun h e => h e.symm))

/-- clearing the head of the descending list leaves its tail -/
theorem pending_clear (p : Nat → Bool) (n i : Nat) (r : List Nat) (h : pendingDesc p n = i :: r) :
    pendingDesc (clear p i) n = r := by
  unfold pendingDesc at *
  have nd : ((List.range n).reverse).Nodup := range_rev_nodup n
  -- split the descending range at i
  generalize hl : (List.range n).reverse = l at h nd
  clear hl
  induction l with
  | nil => simp at h
  | cons x l ih =>
    by_cases hx : p x = true
    · rw [List.filter_cons_of_pos hx] at h
      obtain ⟨rfl, rfl⟩ := List.cons.inj h
      rw [List.filter_cons_of_neg (by simp [clear])]
      exact filter_clear_head l p x nd rfl
    · rw [List.filter_cons_of_neg hx] at h
      have hx' : ¬ clear p i x = true := by simp only [clear]; split <;> simp_all
      rw [List.filter_cons_of_neg hx']
      exact ih h (List.nodup_cons.mp nd).2

/-- Each step makes the next call of the descending pending list; after as many steps as
there are pending calls (and any further steps, which do nothing), the calls made are the
pending ones, highest first, each exactly once. -/
theorem runner_order (n : Nat) (p : Nat → Bool) (tr : List Nat) (k : Nat)
    (hk : (pendingDesc p n).length ≤ k) :
    (steps n k (p, tr)).2 = tr ++ pendingDesc p n := by
  induction k generalizing p tr with
  | zero =>
    have : pendingDesc p n = [] := List.eq_nil_of_length_eq_zero (by omega)
    simp [steps, this]
  | succ k ih =>
    simp only [steps, step]
    cases h : pendingDesc p n with
    | nil =>
      simp only
      -- nothing pending: further steps do nothing
      have : ∀ m st, st.1 = p → (steps n m st).2 = st.2 := by
        intro m
        induction m with
        | zero => intro st _; rfl
        | succ m ihm =>
          intro st hs
          simp only [steps, step, hs, h]
          exact ihm st hs
      rw [this k (p, tr) rfl]; simp
    | cons i r =>
      simp only
      rw [ih (clear p i) (tr ++ [i]) (by rw [pending_clear p n i r h]; rw [h] at hk; simp at hk; omega),
        pending_clear p n i r h]
      simp

/-- the calls made have no duplicates -/
theorem runner_once (p : Nat → Bool) (n : Nat) : (pendingDesc p n).Nodup :=
  List.Pairwise.filter _ (range_rev_nodup n)

end Defer

#print axioms Defer.path_increasing
#print axioms Defer.runner_order
#print axioms Defer.runner_once
