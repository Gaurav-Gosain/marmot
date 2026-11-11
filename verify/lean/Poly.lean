/-
Poly.lean: polynomial inductions in Rule P (docs/loops.md condition 2; Parallelize.cpp).

An integer header phi with ScalarEvolution form {c0, +, c1, +, c2} on N-bit words takes, in
iteration k, the value c0 + c1*k + c2*binomial(k, 2) modulo 2^N (the serial recurrence
v' = v + d, d' = d + c2 with v = c0, d = c1 at k = 0). The chunk function recomputes it from
the 64-bit iteration counter k.

`serial_closed`: the closed form equals the serial recurrence.
`binom_halving`: binomial(k, 2) computed by halving the even one of k and k - 1 first and
multiplying in 64 bits is exact modulo 2^64, hence modulo 2^N after truncation.
`trunc_first_wrong`: computing the halving on k truncated to N bits first (what
Parallelize.cpp did) is wrong: for N = 16 and k = 65536 it gives 0 instead of 32768.
-/

namespace Poly

/-- the serial recurrence: (value, difference) after k iterations, as integers -/
def serial (c0 c1 c2 : Int) : Nat → Int × Int
  | 0 => (c0, c1)
  | k + 1 => ((serial c0 c1 c2 k).1 + (serial c0 c1 c2 k).2, (serial c0 c1 c2 k).2 + c2)

/-- binomial(k, 2) -/
def B (k : Nat) : Nat := k * (k - 1) / 2

theorem sq_step (k : Nat) : (k + 1) * k = k * (k - 1) + 2 * k := by
  rcases k with _ | k
  · rfl
  · simp only [Nat.add_sub_cancel, Nat.add_mul, Nat.mul_add, Nat.mul_one, Nat.one_mul]; omega

theorem B_succ (k : Nat) : B (k + 1) = B k + k := by
  unfold B; rw [Nat.add_sub_cancel, sq_step, Nat.add_mul_div_left _ _ (by decide)]

theorem serial_closed (c0 c1 c2 : Int) (k : Nat) :
    serial c0 c1 c2 k = (c0 + c1 * k + c2 * (B k : Int), c1 + c2 * k) := by
  induction k with
  | zero => simp [serial, B]
  | succ k ih =>
    simp only [serial, ih, Prod.mk.injEq, B_succ]
    push_cast
    simp only [Int.mul_add, Int.mul_one]
    constructor <;> omega

/-- halving the even factor first -/
def halveMul (k : Nat) : Nat := if k % 2 = 0 then (k / 2) * (k - 1) else k * ((k - 1) / 2)

theorem halveMul_eq (k : Nat) : halveMul k = B k := by
  unfold halveMul B
  split
  · rename_i h
    obtain ⟨q, rfl⟩ : ∃ q, k = 2 * q := ⟨k / 2, by omega⟩
    rw [Nat.mul_div_cancel_left _ (by decide), Nat.mul_assoc, Nat.mul_div_cancel_left _ (by decide)]
  · rename_i h
    obtain ⟨q, rfl⟩ : ∃ q, k = 2 * q + 1 := ⟨k / 2, by omega⟩
    simp only [Nat.add_sub_cancel]
    rw [Nat.mul_div_cancel_left _ (by decide), Nat.mul_left_comm (2 * q + 1) 2 q,
      Nat.mul_div_cancel_left _ (by decide)]

/-- Exact modulo 2^64: multiplying the halved factors (each a 64-bit word) with wrapping
64-bit multiplication gives binomial(k, 2) modulo 2^64; truncating to N <= 64 bits then
gives it modulo 2^N. -/
theorem binom_halving (k N : Nat) (hN : N ≤ 64) :
    (if k % 2 = 0 then ((k / 2) % 2^64) * ((k - 1) % 2^64) else (k % 2^64) * (((k - 1) / 2) % 2^64)) % 2^64 % 2^N
      = B k % 2^N := by
  have hd : 2^N ∣ 2^64 := Nat.pow_dvd_pow 2 hN
  rw [← halveMul_eq]
  unfold halveMul
  split <;> rw [← Nat.mul_mod, Nat.mod_mod_of_dvd _ hd]

/-- What Parallelize.cpp computed: the halving on k truncated to N bits. For N = 16 and
k = 65536 that is 0, but binomial(65536, 2) mod 2^16 = 32768. -/
theorem trunc_first_wrong : halveMul (65536 % 2^16) % 2^16 = 0 ∧ B 65536 % 2^16 = 32768 := by
  decide

end Poly

#print axioms Poly.serial_closed
#print axioms Poly.binom_halving
#print axioms Poly.trunc_first_wrong
