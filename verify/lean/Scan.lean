/-
Scan.lean: two loop transforms of backend/lib/Loops (idiom recognition and Rule P), as
equalities of folds over lists, for any operation with the stated laws (integer add, xor, or,
and on words all have them; wrapping arithmetic included).

1. Blocked scan. A loop `r = r op x(i); q(i) = r` stores the inclusive prefixes of x starting
   from r0. The transform splits x into blocks, computes each block's sum on its own (in
   parallel), takes the prefix of the block sums starting from r0 serially (the offsets), and
   rescans every block from its offset (in parallel). `blocked_scan` shows the stored values
   equal the serial loop's, for an associative op (no identity needed: every block is
   non-empty, as the transform's blocks are).

2. Privatized reduction arrays (histogram `h[b(i)] += v(i)`). Each chunk counts into its own
   zeroed copy, then adds every element of its copy into h. `privatized` shows the final h
   equals the serial loop's, for an associative and commutative op with an identity (the zero
   the private copies start from), for any split of the iterations into chunks.
-/

namespace Scan

variable {α : Type} (op : α → α → α)

/-- inclusive prefixes of xs starting from r: r op x0, (r op x0) op x1, ... -/
def scan (r : α) : List α → List α
  | [] => []
  | x :: xs => op r x :: scan (op r x) xs

/-- the value after the whole list -/
def fold (r : α) : List α → α
  | [] => r
  | x :: xs => fold (op r x) xs

theorem scan_append (r : α) (xs ys : List α) :
    scan op r (xs ++ ys) = scan op r xs ++ scan op (fold op r xs) ys := by
  induction xs generalizing r with
  | nil => rfl
  | cons x xs ih => simp [scan, fold, ih]

/-- folding from a op b equals a op (folding from b), for associative op -/
theorem fold_assoc (assoc : ∀ a b c, op (op a b) c = op a (op b c)) (a b : α) (xs : List α) :
    fold op (op a b) xs = op a (fold op b xs) := by
  induction xs generalizing b with
  | nil => rfl
  | cons x xs ih => simp only [fold]; rw [assoc]; exact ih (op b x)

/-- a block's sum computed on its own: its first element folded with the rest -/
def bsum (b : α × List α) : α := fold op b.1 b.2

/-- the transform: offsets r0, r0 op s0, (r0 op s0) op s1, ...; each block rescanned from its
offset -/
def blocked (r : α) : List (α × List α) → List α
  | [] => []
  | b :: bs => scan op r (b.1 :: b.2) ++ blocked (op r (bsum op b)) bs

def flat : List (α × List α) → List α
  | [] => []
  | b :: bs => (b.1 :: b.2) ++ flat bs

theorem blocked_scan (assoc : ∀ a b c, op (op a b) c = op a (op b c)) (r : α)
    (bs : List (α × List α)) : blocked op r bs = scan op r (flat bs) := by
  induction bs generalizing r with
  | nil => rfl
  | cons b bs ih =>
    simp only [blocked, flat]
    rw [scan_append, ih]
    congr 2
    -- the offset after the block: r op (its own sum) = folding the block from r
    simp only [bsum, fold]
    exact (fold_assoc op assoc r b.1 b.2).symm

/-! ## Privatized reduction arrays -/

/-- one update h[j] op= v -/
def upd (h : Nat → α) (j : Nat) (v : α) : Nat → α := fun k => if k = j then op (h k) v else h k

/-- run the updates in order -/
def run (h : Nat → α) : List (Nat × α) → Nat → α
  | [] => h
  | u :: us => run (upd op h u.1 u.2) us

/-- add a private copy into h element by element -/
def merge (h p : Nat → α) : Nat → α := fun k => op (h k) (p k)

/-- the values of the updates to element k, in order -/
def vals (k : Nat) : List (Nat × α) → List α
  | [] => []
  | u :: us => if u.1 = k then u.2 :: vals k us else vals k us

/-- element k after the updates: h k folded with the values of its updates -/
theorem run_elem (h : Nat → α) (us : List (Nat × α)) (k : Nat) :
    run op h us k = fold op (h k) (vals k us) := by
  induction us generalizing h with
  | nil => rfl
  | cons u us ih =>
    simp only [run, vals]
    rw [ih]
    by_cases e : u.1 = k
    · subst e; simp [upd, fold]
    · have : k ≠ u.1 := fun h' => e h'.symm
      simp [upd, this, e]

theorem vals_append (k : Nat) (xs ys : List (Nat × α)) :
    vals k (xs ++ ys) = vals k xs ++ vals k ys := by
  induction xs with
  | nil => rfl
  | cons x xs ih => simp only [List.cons_append, vals]; split <;> simp [ih]

theorem fold_append (r : α) (xs ys : List α) : fold op r (xs ++ ys) = fold op (fold op r xs) ys := by
  induction xs generalizing r with
  | nil => rfl
  | cons x xs ih => simp [fold, ih]

/-- merging a chunk's private copy (zeroed, then the chunk's updates) into h is the same as
running the chunk's updates on h -/
theorem merge_private (assoc : ∀ a b c, op (op a b) c = op a (op b c)) (z : α)
    (hz : ∀ a, op a z = a) (h : Nat → α) (c : List (Nat × α)) :
    merge op h (run op (fun _ => z) c) = run op h c := by
  funext k
  simp only [merge]
  rw [run_elem, run_elem, ← fold_assoc op assoc, hz]

/-- chunks merged in iteration order give the serial result -/
def mergeAll (z : α) (h : Nat → α) : List (List (Nat × α)) → Nat → α
  | [] => h
  | c :: cs => mergeAll z (merge op h (run op (fun _ => z) c)) cs

def joinAll : List (List (Nat × α)) → List (Nat × α)
  | [] => []
  | c :: cs => c ++ joinAll cs

theorem run_append (h : Nat → α) (xs ys : List (Nat × α)) :
    run op h (xs ++ ys) = run op (run op h xs) ys := by
  induction xs generalizing h with
  | nil => rfl
  | cons x xs ih => simp [run, ih]

theorem mergeAll_serial (assoc : ∀ a b c, op (op a b) c = op a (op b c)) (z : α)
    (hz : ∀ a, op a z = a) (h : Nat → α) (cs : List (List (Nat × α))) :
    mergeAll op z h cs = run op h (joinAll cs) := by
  induction cs generalizing h with
  | nil => rfl
  | cons c cs ih =>
    simp only [mergeAll, joinAll]
    rw [merge_private op assoc z hz, ih, run_append]

/-- the atomic adds of different chunks may land in any order: merges commute -/
theorem merge_comm (assoc : ∀ a b c, op (op a b) c = op a (op b c))
    (comm : ∀ a b, op a b = op b a) (h p q : Nat → α) :
    merge op (merge op h p) q = merge op (merge op h q) p := by
  funext k
  simp only [merge]
  rw [assoc, assoc, comm (p k)]

/-- merging the private copies in any order (a permutation of the chunks) gives the serial
result: the theorem for the transform -/
theorem privatized (assoc : ∀ a b c, op (op a b) c = op a (op b c))
    (comm : ∀ a b, op a b = op b a) (z : α) (hz : ∀ a, op a z = a) (h : Nat → α)
    (cs ds : List (List (Nat × α))) (perm : List.Perm cs ds) :
    mergeAll op z h ds = run op h (joinAll cs) := by
  rw [← mergeAll_serial op assoc z hz]
  induction perm generalizing h with
  | nil => rfl
  | cons c _ ih => simp only [mergeAll]; rw [ih]
  | swap c d l => simp only [mergeAll]; rw [merge_comm op assoc comm]
  | trans _ _ ih1 ih2 => rw [ih2, ih1]

end Scan

#print axioms Scan.blocked_scan
#print axioms Scan.privatized
