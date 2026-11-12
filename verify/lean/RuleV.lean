/-
RuleV.lean: Rule V (bounds check versioning), as implemented in
backend/lib/Loops/Versioning.cpp and Affine.cpp.

Two parts.

1. Precondition soundness. A checked value x is split by decomposeAffine as
     x(k, j) = b + coef*k + sum_t s_t * j_t,  0 <= k <= btc,  0 <= j_t <= c_t
   where b, coef and s_t are 64-bit words (read as signed), c_t and btc are counts (unsigned),
   and the j_t are the counters of loops inside L. The value in the IR is that polynomial
   modulo 2^64 (ScalarEvolution's add recurrences are modular), written `wrap` below. The
   precondition evaluates lo, hi, the span coef*btc, min and max with wrapping word
   arithmetic and checks, besides the compare itself:
     every term has |s| * c <= 2^60 (a constant step: c <= 2^60 / |s|; a symbolic step:
       |s| <= 2^30 and c <= 2^30), including the term (coef, btc) of L itself,
     at most 4 terms,
     lo and hi (the invariant part plus the inner terms) are in [-2^61, 2^61] as words.
   We prove that then lo, hi, min and max as computed equal their integer values, every
   value x(k, j) is in the word range (so the IR value is the integer value), and
   min <= x(k, j) <= max. From that, each stay condition the pass emits (x < y, x <= y,
   signed or unsigned, x != y with y invariant, p != nil) holds in every iteration.

2. Versioning. A loop body is a list of instructions: operations, and checks that leave
   the loop with a panic code when their condition is false. The versioned loop runs the
   fast copy (checks dropped) when the precondition holds and the original loop otherwise.
   If the precondition implies that every check passes in every iteration, the versioned
   loop has exactly the outcome of the original: same final state, or the same panic with
   the same code in the same iteration and the same state at that moment.

Trusted (not proved here): ScalarEvolution's decomposition (that the IR value of the
checked value is the polynomial above modulo 2^64, that k stays in [0, btc] and j_t in
[0, c_t]), and the SCEV predicates the pass adds to the precondition.
-/

namespace RuleV

/-! ## Words -/

/-- 2^64 and 2^63, 2^60, 2^61, 2^30 (notations, so that omega sees the numbers) -/
notation "M" => (18446744073709551616 : Int)
notation "H" => (9223372036854775808 : Int)
notation "T60" => (1152921504606846976 : Int)
notation "T61" => (2305843009213693952 : Int)
notation "T30" => (1073741824 : Int)

/-- the signed 64-bit word an integer denotes (two's complement wrap) -/
def wrap (x : Int) : Int := (x + H) % M - H

theorem wrap_id {x : Int} (h1 : -H ≤ x) (h2 : x < H) : wrap x = x := by
  unfold wrap; omega

theorem wrap_range (x : Int) : -H ≤ wrap x ∧ wrap x < H := by
  unfold wrap; omega

/-- A value within 2^62 outside the word range that wraps into [-2^61, 2^61] was already
there: the only other candidates are 2^64 away. -/
theorem wrap_small {x : Int} (h1 : -H - 4 * T60 ≤ x) (h2 : x < H + 4 * T60)
    (w1 : -T61 ≤ wrap x) (w2 : wrap x ≤ T61) : -T61 ≤ x ∧ x ≤ T61 := by
  unfold wrap at w1 w2; omega

/-! ## Terms -/

/-- a term s * j with 0 <= j <= c -/
structure Term where
  s : Int
  c : Int
  j : Int

def Term.ok (t : Term) : Prop := 0 ≤ t.j ∧ t.j ≤ t.c ∧ -T60 ≤ t.s * t.c ∧ t.s * t.c ≤ T60

def Term.lo (t : Term) : Int := min 0 (t.s * t.c)
def Term.hi (t : Term) : Int := max 0 (t.s * t.c)
def Term.val (t : Term) : Int := t.s * t.j

theorem Term.bounds (t : Term) (h : t.ok) : t.lo ≤ t.val ∧ t.val ≤ t.hi ∧ -T60 ≤ t.lo ∧ t.hi ≤ T60 := by
  obtain ⟨hj0, hjc, hl, hh⟩ := h
  unfold Term.lo Term.hi Term.val
  rcases Int.le_total 0 t.s with hs | hs
  · have a : t.s * t.j ≤ t.s * t.c := Int.mul_le_mul_of_nonneg_left hjc hs
    have b : 0 ≤ t.s * t.j := Int.mul_nonneg hs hj0
    omega
  · have a : t.s * t.c ≤ t.s * t.j := Int.mul_le_mul_of_nonpos_left hs hjc
    have b : t.s * t.j ≤ 0 := Int.mul_nonpos_of_nonpos_of_nonneg hs hj0
    omega

/-- The two forms of the term check in Versioning.cpp give |s| * c <= 2^60. -/
theorem const_step_ok (s c : Int) (hs : s ≠ 0) (hsl : s.natAbs ≤ T60) (c0 : 0 ≤ c)
    (hc : c ≤ T60 / s.natAbs) : -T60 ≤ s * c ∧ s * c ≤ T60 := by
  have ha : (0 : Int) < s.natAbs := by omega
  have hq : (T60 / s.natAbs) * s.natAbs ≤ T60 := Int.ediv_mul_le T60 (by omega)
  have h1 : c * s.natAbs ≤ (T60 / s.natAbs) * s.natAbs := Int.mul_le_mul_of_nonneg_right hc (by omega)
  rcases Int.natAbs_eq s with e | e
  · rw [e]; have : (0 : Int) ≤ c * s.natAbs := Int.mul_nonneg c0 (by omega)
    constructor <;> rw [Int.mul_comm] <;> omega
  · rw [e]; have : (0 : Int) ≤ c * s.natAbs := Int.mul_nonneg c0 (by omega)
    rw [Int.neg_mul, Int.mul_comm]; omega

theorem sym_step_ok (s c : Int) (h1 : -T30 ≤ s) (h2 : s ≤ T30) (c0 : 0 ≤ c) (hc : c ≤ T30) :
    -T60 ≤ s * c ∧ s * c ≤ T60 := by
  rcases Int.le_total 0 s with hs | hs
  · have a : s * c ≤ T30 * c := Int.mul_le_mul_of_nonneg_right h2 c0
    have b : T30 * c ≤ T30 * T30 := Int.mul_le_mul_of_nonneg_left hc (by decide)
    have z : 0 ≤ s * c := Int.mul_nonneg hs c0
    omega
  · have a : -T30 * c ≤ s * c := Int.mul_le_mul_of_nonneg_right h1 c0
    have b : -T30 * T30 ≤ -T30 * c := Int.mul_le_mul_of_nonpos_left (by decide) hc
    have z : s * c ≤ 0 := Int.mul_nonpos_of_nonpos_of_nonneg hs c0
    omega

def sumLo : List Term → Int
  | [] => 0
  | t :: r => t.lo + sumLo r

def sumHi : List Term → Int
  | [] => 0
  | t :: r => t.hi + sumHi r

def sumVal : List Term → Int
  | [] => 0
  | t :: r => t.val + sumVal r

def AllOk : List Term → Prop
  | [] => True
  | t :: r => t.ok ∧ AllOk r

theorem sums (ts : List Term) (h : AllOk ts) :
    sumLo ts ≤ sumVal ts ∧ sumVal ts ≤ sumHi ts ∧
    -(ts.length * T60 : Int) ≤ sumLo ts ∧ sumHi ts ≤ ts.length * T60 ∧ sumLo ts ≤ 0 ∧ 0 ≤ sumHi ts := by
  induction ts with
  | nil => simp [sumLo, sumHi, sumVal]
  | cons t r ih =>
    obtain ⟨ht, hr⟩ := h
    have b := t.bounds ht
    have i := ih hr
    have l0 : t.lo ≤ 0 := by unfold Term.lo; omega
    have h0 : 0 ≤ t.hi := by unfold Term.hi; omega
    simp only [sumLo, sumHi, sumVal, List.length_cons]
    push_cast
    omega

/-! ## A checked value and its range -/

/-- x(k, j) = b + coef*k + sum of the inner terms; k is the term (coef, btc, k) -/
structure Aff where
  b : Int
  k : Term
  ts : List Term

/-- the integer value at iteration (k, j) -/
def Aff.val (a : Aff) : Int := a.b + a.k.val + sumVal a.ts
/-- lo and hi: the invariant part plus the inner terms (Affine.cpp) -/
def Aff.lo (a : Aff) : Int := a.b + sumLo a.ts
def Aff.hi (a : Aff) : Int := a.b + sumHi a.ts
/-- min and max over the iteration space (Versioning.cpp, range) -/
def Aff.mn (a : Aff) : Int := a.lo + a.k.lo
def Aff.mx (a : Aff) : Int := a.hi + a.k.hi

/-- What the precondition checks about a checked value (besides the compare): the word
inputs are words, every term is small, at most 4 terms, and lo and hi as computed with
wrapping arithmetic are in [-2^61, 2^61]. -/
structure Aff.Guard (a : Aff) : Prop where
  bw : -H ≤ a.b ∧ a.b < H
  kok : a.k.ok
  tok : AllOk a.ts
  nterms : a.ts.length + 1 ≤ 4
  lo : -T61 ≤ wrap a.lo ∧ wrap a.lo ≤ T61
  hi : -T61 ≤ wrap a.hi ∧ wrap a.hi ≤ T61

/-- Range soundness: under the guard, lo and hi are their integer values, the computed
min and max are their integer values, and every value of x lies in [min, max] and in the
word range, so the IR value (x modulo 2^64) is the integer value. -/
theorem Aff.range (a : Aff) (g : a.Guard) :
    wrap a.mn = a.mn ∧ wrap a.mx = a.mx ∧
    a.mn ≤ a.val ∧ a.val ≤ a.mx ∧ wrap a.val = a.val ∧
    -T61 - T60 ≤ a.mn ∧ a.mx ≤ T61 + T60 := by
  obtain ⟨bw, kok, tok, nt, glo, ghi⟩ := g
  have s := sums a.ts tok
  have kb := a.k.bounds kok
  have hn : (a.ts.length : Int) ≤ 3 := by omega
  have l1 : -(a.ts.length * T60 : Int) ≥ -3 * T60 := by omega
  have h1 : (a.ts.length * T60 : Int) ≤ 3 * T60 := by
    have := Int.mul_le_mul_of_nonneg_right hn (by decide : (0 : Int) ≤ T60); omega
  have rlo := wrap_small (x := a.lo) (by unfold Aff.lo; omega) (by unfold Aff.lo; omega) glo.1 glo.2
  have rhi := wrap_small (x := a.hi) (by unfold Aff.hi; omega) (by unfold Aff.hi; omega) ghi.1 ghi.2
  unfold Aff.mn Aff.mx Aff.val at *
  unfold Aff.lo Aff.hi at *
  refine ⟨wrap_id (by omega) (by omega), wrap_id (by omega) (by omega), by omega, by omega,
    wrap_id (by omega) (by omega), by omega, by omega⟩

/-! ## The stay conditions -/

/-- How the pass turns a check that keeps execution in L into a precondition (checkCondition).
`x` and `y` are the two sides after normalization (x on the left). -/
inductive Stay
  | slt | sle | ult | ule
  /-- x != y with y invariant: x must stay below y -/
  | ne

/-- the stay predicate on the IR values (signed words in [-2^63, 2^63)) -/
def Stay.holds : Stay → Int → Int → Prop
  | .slt, x, y => x < y
  | .sle, x, y => x ≤ y
  | .ult, x, y => (x % M) < (y % M)
  | .ule, x, y => (x % M) ≤ (y % M)
  | .ne, x, y => x ≠ y

/-- the precondition for the stay predicate, on the computed min and max -/
def Stay.pre : Stay → Int → Int → Int → Prop
  | .slt, _, mxx, mny => mxx < mny
  | .sle, _, mxx, mny => mxx ≤ mny
  | .ult, mnx, mxx, mny => 0 ≤ mnx ∧ mxx < mny
  | .ule, mnx, mxx, mny => 0 ≤ mnx ∧ mxx ≤ mny
  | .ne, mnx, mxx, mny => 0 ≤ mnx ∧ mxx < mny

/-- One side of a compare: an affine value in L (with the guard the pass adds), or a value
invariant in L (range() gives min = max = the value and adds no guard). -/
inductive Side
  | aff (a : Aff)
  | inv (v : Int)

def Side.mn : Side → Int
  | .aff a => wrap a.mn
  | .inv v => v

def Side.mx : Side → Int
  | .aff a => wrap a.mx
  | .inv v => v

/-- the IR value in an iteration -/
def Side.val : Side → Int
  | .aff a => wrap a.val
  | .inv v => v

def Side.Guard : Side → Prop
  | .aff a => a.Guard
  | .inv v => -H ≤ v ∧ v < H

theorem Side.range (x : Side) (g : x.Guard) :
    x.mn ≤ x.val ∧ x.val ≤ x.mx ∧ -H ≤ x.val ∧ x.val < H := by
  cases x with
  | aff a =>
    obtain ⟨hmn, hmx, h1, h2, hv, h3, h4⟩ := a.range g
    simp only [Side.mn, Side.mx, Side.val]; rw [hmn, hmx, hv]; omega
  | inv v => simp only [Side.mn, Side.mx, Side.val, Side.Guard] at *; omega

/-- Precondition soundness (checkCondition): for sides x and y (each affine or invariant),
the precondition on the computed min and max implies the stay predicate on the IR values
in every iteration of the iteration space. -/
theorem stay_sound (st : Stay) (x y : Side) (gx : x.Guard) (gy : y.Guard)
    (pre : st.pre x.mn x.mx y.mn) : st.holds x.val y.val := by
  have rx := x.range gx
  have ry := y.range gy
  cases st <;> simp only [Stay.pre, Stay.holds] at * <;> omega

/-- Nil check on an address p (as an integer): the precondition min(p) > 0 implies p != 0 in
every iteration. -/
theorem nil_sound (p : Side) (g : p.Guard) (pre : 0 < p.mn) : p.val ≠ 0 := by
  have := p.range g; omega

/- When both sides are invariant in L the pass puts the compare itself into the
precondition; stay_sound with two `inv` sides covers that too (for the signed forms; for
the unsigned forms the pass uses the compare directly, which is the stay predicate). -/

/-! ## Versioning -/

inductive Out (σ : Type) where
  | ok (s : σ)
  | panic (code : Nat) (s : σ)
  deriving Inhabited

/-- One instruction of the loop body. A check's condition may depend on the iteration and
on the state (a loaded bound); when it is false the loop exits to the panic with `code`. -/
inductive Instr (σ : Type) where
  | op (f : Nat → σ → σ)
  | check (c : Nat → σ → Bool) (code : Nat)

/-- the body at iteration k; `fast` drops the checks (the fast copy) -/
def runBody {σ : Type} (fast : Bool) (k : Nat) : List (Instr σ) → σ → Out σ
  | [], s => .ok s
  | .op f :: r, s => runBody fast k r (f k s)
  | .check c code :: r, s =>
    if fast then runBody fast k r s
    else if c k s then runBody fast k r s else .panic code s

/-- iterations k, k+1, ..., k+n-1 -/
def runLoop {σ : Type} (fast : Bool) (body : List (Instr σ)) : Nat → Nat → σ → Out σ
  | 0, _, s => .ok s
  | n + 1, k, s =>
    match runBody fast k body s with
    | .ok s' => runLoop fast body n (k + 1) s'
    | .panic code s' => .panic code s'

/-- every check the original body evaluates at iteration k passes -/
def bodyPasses {σ : Type} (k : Nat) : List (Instr σ) → σ → Prop
  | [], _ => True
  | .op f :: r, s => bodyPasses k r (f k s)
  | .check c _ :: r, s => c k s = true ∧ bodyPasses k r s

/-- every check passes in every iteration of the run of the original loop -/
def loopPasses {σ : Type} (body : List (Instr σ)) : Nat → Nat → σ → Prop
  | 0, _, _ => True
  | n + 1, k, s =>
    bodyPasses k body s ∧
    match runBody false k body s with
    | .ok s' => loopPasses body n (k + 1) s'
    | .panic _ _ => True

theorem body_eq {σ : Type} (k : Nat) (body : List (Instr σ)) (s : σ) (h : bodyPasses k body s) :
    runBody true k body s = runBody false k body s := by
  induction body generalizing s with
  | nil => rfl
  | cons i r ih =>
    cases i with
    | op f => exact ih _ h
    | check c code =>
      obtain ⟨hc, hr⟩ := h
      simp only [runBody, hc, if_true]
      exact ih s hr

/-- when every check passes, the original body does not panic -/
theorem body_ok {σ : Type} (k : Nat) (body : List (Instr σ)) (s : σ) (h : bodyPasses k body s) :
    ∃ s', runBody false k body s = .ok s' := by
  induction body generalizing s with
  | nil => exact ⟨s, rfl⟩
  | cons i r ih =>
    cases i with
    | op f => exact ih _ h
    | check c code =>
      obtain ⟨hc, hr⟩ := h
      simp only [runBody, hc, if_true, Bool.false_eq_true, if_false]
      exact ih s hr

theorem loop_eq {σ : Type} (body : List (Instr σ)) (n k : Nat) (s : σ) (h : loopPasses body n k s) :
    runLoop true body n k s = runLoop false body n k s := by
  induction n generalizing k s with
  | zero => rfl
  | succ n ih =>
    obtain ⟨hb, hr⟩ := h
    obtain ⟨s', hs'⟩ := body_ok k body s hb
    simp only [runLoop, body_eq k body s hb, hs']
    rw [hs'] at hr
    exact ih (k + 1) s' hr

/-- the versioned loop -/
def versioned {σ : Type} (pre : Bool) (body : List (Instr σ)) (n : Nat) (s : σ) : Out σ :=
  if pre then runLoop true body n 0 s else runLoop false body n 0 s

/-- Rule V: if the precondition implies that every check passes in every iteration, the
versioned loop is observably the original loop (same final state, or the same panic code
at the same moment with the same state). When the precondition is false, the original
loop runs and panics exactly where it would have. -/
theorem versioned_eq {σ : Type} (pre : Bool) (body : List (Instr σ)) (n : Nat) (s : σ)
    (sound : pre = true → loopPasses body n 0 s) :
    versioned pre body n s = runLoop false body n 0 s := by
  unfold versioned
  cases pre with
  | false => rfl
  | true => simp only [if_true]; exact loop_eq body n 0 s (sound rfl)

/-- A check whose condition does not depend on the state (the affine case) and holds for
every iteration below n gives loopPasses. This connects part 1 to part 2: stay_sound shows
the condition for every k in [0, btc]. -/
theorem passes_of_pointwise {σ : Type} (body : List (Instr σ)) (n k : Nat) (s : σ)
    (h : ∀ k', k ≤ k' → k' < k + n → ∀ s', bodyPasses k' body s') : loopPasses body n k s := by
  induction n generalizing k s with
  | zero => trivial
  | succ n ih =>
    refine ⟨h k (Nat.le_refl _) (by omega) s, ?_⟩
    split
    · exact ih (k + 1) _ (fun k' a b s' => h k' (by omega) (by omega) s')
    · trivial

end RuleV


#print axioms RuleV.versioned_eq
#print axioms RuleV.stay_sound
#print axioms RuleV.nil_sound
