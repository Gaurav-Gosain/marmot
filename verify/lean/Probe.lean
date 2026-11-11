/-
Probe.lean: triangular probing (lib/src/runtime/map.go: probing visits groups in triangular
order, which covers every group of a power of two table).

The i-th probe of a table with 2^k groups is at (h + i(i+1)/2) mod 2^k. `probe_injective`:
the first 2^k probes are pairwise different, so they visit every group exactly once; an
insert into a table with a free slot always finds it, and a lookup that stops at an empty
slot has seen every slot the key could have been put in.
-/

namespace Probe

def T (i : Nat) : Nat := i * (i + 1) / 2

theorem two_T (i : Nat) : 2 * T i = i * (i + 1) := by
  unfold T
  have : i * (i + 1) % 2 = 0 := by
    rw [Nat.mul_mod]; rcases Nat.mod_two_eq_zero_or_one i with h | h <;> simp [h, Nat.add_mod]
  omega

theorem odd_coprime (a : Nat) (h : a % 2 = 1) (k : Nat) : Nat.Coprime (2 ^ k) a := by
  apply Nat.Coprime.pow_left
  show Nat.gcd 2 a = 1
  rw [Nat.gcd_rec, h]; rfl

theorem expand (i a : Nat) : (i + a) * (i + a + 1) = i * (i + 1) + a * (2 * i + a + 1) := by
  simp only [Nat.mul_add, Nat.add_mul, Nat.mul_one, Nat.one_mul, Nat.mul_comm a i, Nat.two_mul]
  omega

theorem probe_injective (k h i j : Nat) (hi : i < 2 ^ k) (hj : j < 2 ^ k) (hne : i ≠ j)
    (heq : (h + T i) % 2 ^ k = (h + T j) % 2 ^ k) : False := by
  -- wlog i < j
  rcases Nat.lt_or_gt_of_ne hne with hl | hl
  case' inr => exact probe_injective k h j i hj hi (Ne.symm hne) heq.symm
  obtain ⟨a, rfl⟩ : ∃ a, j = i + a := ⟨j - i, by omega⟩
  have ha : 0 < a := by omega
  -- 2^k divides T j - T i
  have hd : 2 ^ k ∣ T (i + a) - T i := by
    have hle : T i ≤ T (i + a) := by
      have := two_T i; have := two_T (i + a); have := expand i a
      have : 0 ≤ a * (2 * i + a + 1) := Nat.zero_le _
      omega
    have e1 : h + T (i + a) = (h + T i) + (T (i + a) - T i) := by omega
    rw [e1] at heq
    have := Nat.sub_mod_eq_zero_of_mod_eq heq.symm
    rw [Nat.add_sub_cancel_left] at this
    exact Nat.dvd_of_mod_eq_zero this
  -- so 2^(k+1) divides a * (2i + a + 1)
  have hd2 : 2 ^ (k + 1) ∣ a * (2 * i + a + 1) := by
    have e : a * (2 * i + a + 1) = 2 * (T (i + a) - T i) := by
      have := two_T i; have := two_T (i + a); have := expand i a
      rw [Nat.mul_sub]; omega
    rw [e, Nat.pow_succ, Nat.mul_comm (2 ^ k) 2]
    exact Nat.mul_dvd_mul_left 2 hd
  -- one factor is odd; the other is positive and below 2^(k+1)
  have hb : 2 * i + a + 1 < 2 ^ (k + 1) := by rw [Nat.pow_succ]; omega
  rcases Nat.mod_two_eq_zero_or_one a with hpa | hpa
  · have hodd : (2 * i + a + 1) % 2 = 1 := by omega
    have := (odd_coprime _ hodd (k + 1)).dvd_of_dvd_mul_right hd2
    have hpos := Nat.le_of_dvd ha this
    have : a < 2 ^ (k + 1) := by rw [Nat.pow_succ]; omega
    omega
  · have := (odd_coprime _ hpa (k + 1)).dvd_of_dvd_mul_left hd2
    have hpos := Nat.le_of_dvd (by omega) this
    omega

end Probe

#print axioms Probe.probe_injective
