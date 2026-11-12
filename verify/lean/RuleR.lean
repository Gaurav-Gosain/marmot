/-
RuleR.lean: reductions in Rule P (condition 3; backend/lib/Loops/Parallelize.cpp).

The pass splits an integer reduction r = r op x over the iterations: each chunk starts from
the identity of op (Parallelize.cpp, `identity`), folds its own iterations in order, and
combines its partial result into the reduction slot (which holds the start value) with one
atomic read-modify-write, in whatever order the chunks finish.

`split_eq`: for an associative, commutative op with a left identity, every partition of the
iterations into consecutive chunks and every order of combining the partials gives the
serial result. Instances: add (and sub, as add of the negation), xor, or, and, signed and
unsigned min and max, on w-bit words, with the identities the pass uses.

`float_add_not_assoc`: IEEE binary64 addition is not associative, so the same split of a
float sum changes the result; the pass never splits float reductions. This one fact is
checked by evaluation with `native_decide` (Lean's Float is the machine's binary64), which
adds an axiom per use (shown by #print axioms) that the compiled evaluation is right: it trusts Lean's compiler and the hardware's float add.
-/

namespace RuleR

/-! ## Split and combine -/

section
variable {α : Type} (op : α → α → α) (e : α)
  (assoc : ∀ a b c, op (op a b) c = op a (op b c))
  (comm : ∀ a b, op a b = op b a)
  (ident : ∀ a, op e a = a)

include assoc comm ident in
/-- folding from s is s op (fold from the identity) -/
theorem fold_from (l : List α) (s : α) : l.foldl op s = op s (l.foldl op e) := by
  induction l generalizing s with
  | nil => simp only [List.foldl_nil]; rw [comm, ident]
  | cons x r ih =>
    simp only [List.foldl_cons]
    rw [ih (op s x), ih (op e x), ident, assoc]

include assoc comm in
/-- folding is invariant under permutation -/
theorem fold_perm {l l' : List α} (p : l.Perm l') (s : α) : l.foldl op s = l'.foldl op s := by
  induction p generalizing s with
  | nil => rfl
  | cons x _ ih => simp only [List.foldl_cons]; exact ih _
  | swap x y r =>
    simp only [List.foldl_cons]
    congr 1
    rw [assoc, assoc, comm y x]
  | trans _ _ ih1 ih2 => rw [ih1, ih2]

include assoc comm ident in
/-- combining the chunk partials in chunk order is folding the whole sequence -/
theorem fold_chunks (cs : List (List α)) (s : α) :
    (cs.map fun c => c.foldl op e).foldl op s = cs.flatten.foldl op s := by
  induction cs generalizing s with
  | nil => rfl
  | cons c r ih =>
    simp only [List.map_cons, List.foldl_cons, List.flatten_cons, List.foldl_append]
    rw [ih, fold_from op e assoc comm ident c s]

include assoc comm ident in
/-- The split: chunks cs (consecutive pieces of the iteration sequence, `cs.flatten`),
partials folded from the identity, combined into s in any order `order`. -/
theorem split_eq (cs : List (List α)) (order : List α)
    (hp : order.Perm (cs.map fun c => c.foldl op e)) (s : α) :
    order.foldl op s = cs.flatten.foldl op s := by
  rw [fold_perm op assoc comm hp s, fold_chunks op e assoc comm ident]
end

/-! ## The operations the pass accepts, on w-bit words -/

section words
variable {w : Nat}

theorem add_ok : (∀ a b c : BitVec w, a + b + c = a + (b + c)) ∧ (∀ a b : BitVec w, a + b = b + a) ∧
    (∀ a : BitVec w, 0#w + a = a) :=
  ⟨BitVec.add_assoc, BitVec.add_comm, BitVec.zero_add⟩

theorem xor_ok : (∀ a b c : BitVec w, a ^^^ b ^^^ c = a ^^^ (b ^^^ c)) ∧
    (∀ a b : BitVec w, a ^^^ b = b ^^^ a) ∧ (∀ a : BitVec w, 0#w ^^^ a = a) :=
  ⟨BitVec.xor_assoc, BitVec.xor_comm, fun _ => BitVec.zero_xor⟩

theorem or_ok : (∀ a b c : BitVec w, a ||| b ||| c = a ||| (b ||| c)) ∧
    (∀ a b : BitVec w, a ||| b = b ||| a) ∧ (∀ a : BitVec w, 0#w ||| a = a) :=
  ⟨BitVec.or_assoc, BitVec.or_comm, fun _ => BitVec.zero_or⟩

theorem and_ok : (∀ a b c : BitVec w, a &&& b &&& c = a &&& (b &&& c)) ∧
    (∀ a b : BitVec w, a &&& b = b &&& a) ∧ (∀ a : BitVec w, BitVec.allOnes w &&& a = a) :=
  ⟨BitVec.and_assoc, BitVec.and_comm, fun _ => BitVec.allOnes_and⟩

/-- sub: r - x is r + (-x), so a chain of subtractions is the add reduction of the negated
operands; a chunk that starts from 0 and subtracts its x's holds minus their sum, and the
pass combines it with an atomic add. -/
theorem sub_as_add (l : List (BitVec w)) (s : BitVec w) :
    l.foldl (fun r x => r - x) s = (l.map fun x => -x).foldl (· + ·) s := by
  induction l generalizing s with
  | nil => rfl
  | cons x r ih => simp only [List.foldl_cons, List.map_cons]; rw [ih, BitVec.sub_eq_add_neg]

def umin (a b : BitVec w) : BitVec w := if a.toNat ≤ b.toNat then a else b
def umax (a b : BitVec w) : BitVec w := if b.toNat ≤ a.toNat then a else b
def smin (a b : BitVec w) : BitVec w := if a.toInt ≤ b.toInt then a else b
def smax (a b : BitVec w) : BitVec w := if b.toInt ≤ a.toInt then a else b

theorem umin_ok : (∀ a b c : BitVec w, umin (umin a b) c = umin a (umin b c)) ∧
    (∀ a b : BitVec w, umin a b = umin b a) ∧ (∀ a : BitVec w, umin (BitVec.allOnes w) a = a) := by
  refine ⟨fun a b c => ?_, fun a b => ?_, fun a => ?_⟩
  · unfold umin; split <;> split <;> (try split) <;> (try split) <;> first | rfl | omega
  · unfold umin; split <;> split <;> first | rfl | (apply BitVec.eq_of_toNat_eq; omega)
  · unfold umin; split
    · rename_i h; apply BitVec.eq_of_toNat_eq
      have := a.isLt; simp only [BitVec.toNat_allOnes] at h ⊢; omega
    · rfl

theorem umax_ok : (∀ a b c : BitVec w, umax (umax a b) c = umax a (umax b c)) ∧
    (∀ a b : BitVec w, umax a b = umax b a) ∧ (∀ a : BitVec w, umax 0#w a = a) := by
  refine ⟨fun a b c => ?_, fun a b => ?_, fun a => ?_⟩
  · unfold umax; split <;> split <;> (try split) <;> (try split) <;> first | rfl | omega
  · unfold umax; split <;> split <;> first | rfl | (apply BitVec.eq_of_toNat_eq; omega)
  · unfold umax; split
    · rename_i h; apply BitVec.eq_of_toNat_eq; simp only [BitVec.toNat_ofNat, Nat.zero_mod] at h ⊢; omega
    · rfl

theorem smin_ok : (∀ a b c : BitVec w, smin (smin a b) c = smin a (smin b c)) ∧
    (∀ a b : BitVec w, smin a b = smin b a) ∧ (∀ a : BitVec w, smin (BitVec.intMax w) a = a) := by
  refine ⟨fun a b c => ?_, fun a b => ?_, fun a => ?_⟩
  · unfold smin; split <;> split <;> (try split) <;> (try split) <;> first | rfl | omega
  · unfold smin; split <;> split <;> first | rfl | (apply BitVec.eq_of_toInt_eq; omega)
  · unfold smin; split
    · rename_i h; apply BitVec.eq_of_toInt_eq
      have := BitVec.toInt_le (x := a); rw [BitVec.toInt_intMax] at h ⊢; omega
    · rfl

theorem smax_ok : (∀ a b c : BitVec w, smax (smax a b) c = smax a (smax b c)) ∧
    (∀ a b : BitVec w, smax a b = smax b a) ∧ (∀ a : BitVec w, smax (BitVec.intMin w) a = a) := by
  refine ⟨fun a b c => ?_, fun a b => ?_, fun a => ?_⟩
  · unfold smax; split <;> split <;> (try split) <;> (try split) <;> first | rfl | omega
  · unfold smax; split <;> split <;> first | rfl | (apply BitVec.eq_of_toInt_eq; omega)
  · unfold smax; split
    · rename_i h; apply BitVec.eq_of_toInt_eq
      have := BitVec.toInt_intMin_le a; omega
    · rfl

end words

/-! ## Floating point add is excluded -/

/-- (1 + 2^53) + (-2^53) = 0 but 1 + (2^53 + (-2^53)) = 1 in IEEE binary64: a chunk holding
the last two iterations would change the sum. -/
theorem float_add_not_assoc :
    ((1.0 : Float) + 9007199254740992.0) + (-9007199254740992.0) == 0.0 ∧
    (1.0 : Float) + (9007199254740992.0 + (-9007199254740992.0)) == 1.0 := by
  native_decide

/-- So split_eq's hypothesis fails for float add, and the split can differ from the serial
sum: serial [2^53, -2^53] from 1 is 0, the chunk partial of the same two iterations
combined into 1 is 1. -/
theorem float_split_differs :
    ([9007199254740992.0, -9007199254740992.0].foldl (· + ·) (1.0 : Float)) == 0.0 ∧
    ((1.0 : Float) + [9007199254740992.0, -9007199254740992.0].foldl (· + ·) (-0.0)) == 1.0 := by
  native_decide

end RuleR

#print axioms RuleR.split_eq
#print axioms RuleR.smin_ok
#print axioms RuleR.float_split_differs
