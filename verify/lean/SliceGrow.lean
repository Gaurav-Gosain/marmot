/-
SliceGrow.lean: slice growth (lib/src/runtime/slice.go, nextslicecap and growslice).

`nextslicecap_ge`: growslice is called for append when newLen > oldCap >= 0. Every exit of
nextslicecap returns at least newLen, with Go's int as a wrapping 64-bit integer (the loop's
`newcap += (newcap + 768) >> 2` may wrap; the loop then leaves through the unsigned compare
and the `newcap <= 0` fallback). The loop is modeled with fuel: whatever number of rounds it
runs before it exits, the result is at least newLen.

`grow_fits`: with roundupsize(x) >= x (a property of the size class table, not proved
here), the capacity growslice returns, roundupsize(newcap * size) / size, is at least newcap,
hence at least newLen. verify/testdata/progs/appendcap.go checks cap >= len, kept elements
and an unchanged old array on real output (capacities themselves are implementation
defined: stock Go 1.25 starts small non-escaping slices on the stack).

Not covered: the overflow check against maxAlloc and the copy of the old elements.
-/

namespace SliceGrow

/-- 64-bit two's complement wrap -/
def wrap (x : Int) : Int := (x + 2^63) % 2^64 - 2^63

/-- unsigned view of a signed 64-bit value -/
def u (x : Int) : Int := x % 2^64

/-- the loop: `newcap += (newcap + 3*256) >> 2` until uint(newcap) >= uint(newLen). The shift
of a signed value is arithmetic: floor division by 4. -/
def loop (newLen : Int) : Nat → Int → Option Int
  | 0, _ => none
  | fuel + 1, c =>
    let c' := wrap (c + (wrap (c + 768)) / 4)
    if u c' ≥ u newLen then some c' else loop newLen fuel c'

def nextslicecap (newLen oldCap : Int) (fuel : Nat) : Option Int :=
  let doublecap := wrap (oldCap + oldCap)
  if newLen > doublecap then some newLen
  else if oldCap < 256 then some doublecap
  else match loop newLen fuel oldCap with
    | none => none
    | some c => if c ≤ 0 then some newLen else some c

theorem wrap_range (x : Int) : -2^63 ≤ wrap x ∧ wrap x < 2^63 := by unfold wrap; omega

theorem loop_exit (newLen : Int) (fuel : Nat) (c r : Int) (h : loop newLen fuel c = some r) :
    u r ≥ u newLen ∧ -2^63 ≤ r ∧ r < 2^63 := by
  induction fuel generalizing c with
  | zero => simp [loop] at h
  | succ f ih =>
    simp only [loop] at h
    split at h
    · cases h; exact ⟨by assumption, wrap_range _⟩
    · exact ih _ h

theorem nextslicecap_ge (newLen oldCap : Int) (fuel : Nat) (r : Int)
    (h0 : 0 ≤ oldCap) (h1 : oldCap < newLen) (h2 : newLen < 2^63)
    (h : nextslicecap newLen oldCap fuel = some r) : newLen ≤ r := by
  unfold nextslicecap at h
  simp only at h
  split at h
  · cases h; exact Int.le_refl _
  · split at h
    · cases h
      -- oldCap < 256: no wrap, doublecap = 2*oldCap >= newLen
      rename_i hn _
      have : wrap (oldCap + oldCap) = oldCap + oldCap := by unfold wrap; omega
      omega
    · split at h
      · cases h
      · rename_i c hc
        have := loop_exit newLen fuel oldCap c hc
        split at h
        · cases h; exact Int.le_refl _
        · cases h; unfold u at this; omega

/-- growslice's capacity from the rounded allocation size -/
theorem grow_fits (newcap size rounded : Nat) (hs : 0 < size) (hr : newcap * size ≤ rounded) :
    newcap ≤ rounded / size := (Nat.le_div_iff_mul_le hs).mpr hr

end SliceGrow

#print axioms SliceGrow.nextslicecap_ge
#print axioms SliceGrow.grow_fits
