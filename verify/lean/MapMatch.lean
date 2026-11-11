import Std.Tactic.BVDecide

/-
MapMatch.lean: the control word matches of the map (lib/src/runtime/map.go, matchH2,
matchEmpty, matchFull), checked for all 64-bit control words by bit blasting (bv_decide:
Lean's SAT based decision procedure; the SAT solver's LRAT certificate is checked by a
verified checker run as compiled code, so each theorem has an axiom that trusts Lean's
compiler, shown by #print axioms).

Hypothesis (the table invariant): every control byte is ctrlEmpty (0x80), ctrlDeleted
(0xFE) or a full slot's h2 (below 0x80), and h2 < 0x80.

- `empty_exact`: matchEmpty sets bit 7 of byte i exactly when byte i is empty.
- `full_exact`: matchFull sets bit 7 of byte i exactly when byte i is full.
- `h2_complete`: matchH2 sets bit 7 of every byte equal to h2 (no false negatives; false
  positives are allowed, the caller compares keys).
- `h2_only_full`: matchH2 never reports an empty or deleted slot.
- All matches have no bits outside the msbs.
-/

namespace MapMatch

def lsbs : BitVec 64 := 0x0101010101010101#64
def msbs : BitVec 64 := 0x8080808080808080#64

def matchH2 (ctrl h2 : BitVec 64) : BitVec 64 :=
  let v := ctrl ^^^ (lsbs * h2)
  ((v - lsbs) &&& ~~~v) &&& msbs

def matchEmpty (ctrl : BitVec 64) : BitVec 64 := (ctrl &&& ~~~(ctrl <<< 6)) &&& msbs

def matchFull (ctrl : BitVec 64) : BitVec 64 := ~~~ctrl &&& msbs

theorem empty_exact (c h : BitVec 64) (hv : (((c >>> 0) &&& 0xFF#64) = 0x80#64 ∨ ((c >>> 0) &&& 0xFF#64) = 0xFE#64 ∨ ((c >>> 0) &&& 0xFF#64) < 0x80#64) ∧ (((c >>> 8) &&& 0xFF#64) = 0x80#64 ∨ ((c >>> 8) &&& 0xFF#64) = 0xFE#64 ∨ ((c >>> 8) &&& 0xFF#64) < 0x80#64) ∧ (((c >>> 16) &&& 0xFF#64) = 0x80#64 ∨ ((c >>> 16) &&& 0xFF#64) = 0xFE#64 ∨ ((c >>> 16) &&& 0xFF#64) < 0x80#64) ∧ (((c >>> 24) &&& 0xFF#64) = 0x80#64 ∨ ((c >>> 24) &&& 0xFF#64) = 0xFE#64 ∨ ((c >>> 24) &&& 0xFF#64) < 0x80#64) ∧ (((c >>> 32) &&& 0xFF#64) = 0x80#64 ∨ ((c >>> 32) &&& 0xFF#64) = 0xFE#64 ∨ ((c >>> 32) &&& 0xFF#64) < 0x80#64) ∧ (((c >>> 40) &&& 0xFF#64) = 0x80#64 ∨ ((c >>> 40) &&& 0xFF#64) = 0xFE#64 ∨ ((c >>> 40) &&& 0xFF#64) < 0x80#64) ∧ (((c >>> 48) &&& 0xFF#64) = 0x80#64 ∨ ((c >>> 48) &&& 0xFF#64) = 0xFE#64 ∨ ((c >>> 48) &&& 0xFF#64) < 0x80#64) ∧ (((c >>> 56) &&& 0xFF#64) = 0x80#64 ∨ ((c >>> 56) &&& 0xFF#64) = 0xFE#64 ∨ ((c >>> 56) &&& 0xFF#64) < 0x80#64)) (hh : h < 0x80#64) :
    (((matchEmpty c >>> 7) &&& 1#64) = 1#64 ↔ ((c >>> 0) &&& 0xFF#64) = 0x80#64) ∧ (((matchEmpty c >>> 15) &&& 1#64) = 1#64 ↔ ((c >>> 8) &&& 0xFF#64) = 0x80#64) ∧ (((matchEmpty c >>> 23) &&& 1#64) = 1#64 ↔ ((c >>> 16) &&& 0xFF#64) = 0x80#64) ∧ (((matchEmpty c >>> 31) &&& 1#64) = 1#64 ↔ ((c >>> 24) &&& 0xFF#64) = 0x80#64) ∧ (((matchEmpty c >>> 39) &&& 1#64) = 1#64 ↔ ((c >>> 32) &&& 0xFF#64) = 0x80#64) ∧ (((matchEmpty c >>> 47) &&& 1#64) = 1#64 ↔ ((c >>> 40) &&& 0xFF#64) = 0x80#64) ∧ (((matchEmpty c >>> 55) &&& 1#64) = 1#64 ↔ ((c >>> 48) &&& 0xFF#64) = 0x80#64) ∧ (((matchEmpty c >>> 63) &&& 1#64) = 1#64 ↔ ((c >>> 56) &&& 0xFF#64) = 0x80#64) := by
  simp only [matchEmpty, lsbs, msbs]
  bv_decide

theorem full_exact (c h : BitVec 64) (hv : (((c >>> 0) &&& 0xFF#64) = 0x80#64 ∨ ((c >>> 0) &&& 0xFF#64) = 0xFE#64 ∨ ((c >>> 0) &&& 0xFF#64) < 0x80#64) ∧ (((c >>> 8) &&& 0xFF#64) = 0x80#64 ∨ ((c >>> 8) &&& 0xFF#64) = 0xFE#64 ∨ ((c >>> 8) &&& 0xFF#64) < 0x80#64) ∧ (((c >>> 16) &&& 0xFF#64) = 0x80#64 ∨ ((c >>> 16) &&& 0xFF#64) = 0xFE#64 ∨ ((c >>> 16) &&& 0xFF#64) < 0x80#64) ∧ (((c >>> 24) &&& 0xFF#64) = 0x80#64 ∨ ((c >>> 24) &&& 0xFF#64) = 0xFE#64 ∨ ((c >>> 24) &&& 0xFF#64) < 0x80#64) ∧ (((c >>> 32) &&& 0xFF#64) = 0x80#64 ∨ ((c >>> 32) &&& 0xFF#64) = 0xFE#64 ∨ ((c >>> 32) &&& 0xFF#64) < 0x80#64) ∧ (((c >>> 40) &&& 0xFF#64) = 0x80#64 ∨ ((c >>> 40) &&& 0xFF#64) = 0xFE#64 ∨ ((c >>> 40) &&& 0xFF#64) < 0x80#64) ∧ (((c >>> 48) &&& 0xFF#64) = 0x80#64 ∨ ((c >>> 48) &&& 0xFF#64) = 0xFE#64 ∨ ((c >>> 48) &&& 0xFF#64) < 0x80#64) ∧ (((c >>> 56) &&& 0xFF#64) = 0x80#64 ∨ ((c >>> 56) &&& 0xFF#64) = 0xFE#64 ∨ ((c >>> 56) &&& 0xFF#64) < 0x80#64)) (hh : h < 0x80#64) :
    (((matchFull c >>> 7) &&& 1#64) = 1#64 ↔ ((c >>> 0) &&& 0xFF#64) < 0x80#64) ∧ (((matchFull c >>> 15) &&& 1#64) = 1#64 ↔ ((c >>> 8) &&& 0xFF#64) < 0x80#64) ∧ (((matchFull c >>> 23) &&& 1#64) = 1#64 ↔ ((c >>> 16) &&& 0xFF#64) < 0x80#64) ∧ (((matchFull c >>> 31) &&& 1#64) = 1#64 ↔ ((c >>> 24) &&& 0xFF#64) < 0x80#64) ∧ (((matchFull c >>> 39) &&& 1#64) = 1#64 ↔ ((c >>> 32) &&& 0xFF#64) < 0x80#64) ∧ (((matchFull c >>> 47) &&& 1#64) = 1#64 ↔ ((c >>> 40) &&& 0xFF#64) < 0x80#64) ∧ (((matchFull c >>> 55) &&& 1#64) = 1#64 ↔ ((c >>> 48) &&& 0xFF#64) < 0x80#64) ∧ (((matchFull c >>> 63) &&& 1#64) = 1#64 ↔ ((c >>> 56) &&& 0xFF#64) < 0x80#64) := by
  simp only [matchFull, lsbs, msbs]
  bv_decide

theorem h2_complete (c h : BitVec 64) (hv : (((c >>> 0) &&& 0xFF#64) = 0x80#64 ∨ ((c >>> 0) &&& 0xFF#64) = 0xFE#64 ∨ ((c >>> 0) &&& 0xFF#64) < 0x80#64) ∧ (((c >>> 8) &&& 0xFF#64) = 0x80#64 ∨ ((c >>> 8) &&& 0xFF#64) = 0xFE#64 ∨ ((c >>> 8) &&& 0xFF#64) < 0x80#64) ∧ (((c >>> 16) &&& 0xFF#64) = 0x80#64 ∨ ((c >>> 16) &&& 0xFF#64) = 0xFE#64 ∨ ((c >>> 16) &&& 0xFF#64) < 0x80#64) ∧ (((c >>> 24) &&& 0xFF#64) = 0x80#64 ∨ ((c >>> 24) &&& 0xFF#64) = 0xFE#64 ∨ ((c >>> 24) &&& 0xFF#64) < 0x80#64) ∧ (((c >>> 32) &&& 0xFF#64) = 0x80#64 ∨ ((c >>> 32) &&& 0xFF#64) = 0xFE#64 ∨ ((c >>> 32) &&& 0xFF#64) < 0x80#64) ∧ (((c >>> 40) &&& 0xFF#64) = 0x80#64 ∨ ((c >>> 40) &&& 0xFF#64) = 0xFE#64 ∨ ((c >>> 40) &&& 0xFF#64) < 0x80#64) ∧ (((c >>> 48) &&& 0xFF#64) = 0x80#64 ∨ ((c >>> 48) &&& 0xFF#64) = 0xFE#64 ∨ ((c >>> 48) &&& 0xFF#64) < 0x80#64) ∧ (((c >>> 56) &&& 0xFF#64) = 0x80#64 ∨ ((c >>> 56) &&& 0xFF#64) = 0xFE#64 ∨ ((c >>> 56) &&& 0xFF#64) < 0x80#64)) (hh : h < 0x80#64) :
    (((c >>> 0) &&& 0xFF#64) = h → ((matchH2 c h >>> 7) &&& 1#64) = 1#64) ∧ (((c >>> 8) &&& 0xFF#64) = h → ((matchH2 c h >>> 15) &&& 1#64) = 1#64) ∧ (((c >>> 16) &&& 0xFF#64) = h → ((matchH2 c h >>> 23) &&& 1#64) = 1#64) ∧ (((c >>> 24) &&& 0xFF#64) = h → ((matchH2 c h >>> 31) &&& 1#64) = 1#64) ∧ (((c >>> 32) &&& 0xFF#64) = h → ((matchH2 c h >>> 39) &&& 1#64) = 1#64) ∧ (((c >>> 40) &&& 0xFF#64) = h → ((matchH2 c h >>> 47) &&& 1#64) = 1#64) ∧ (((c >>> 48) &&& 0xFF#64) = h → ((matchH2 c h >>> 55) &&& 1#64) = 1#64) ∧ (((c >>> 56) &&& 0xFF#64) = h → ((matchH2 c h >>> 63) &&& 1#64) = 1#64) := by
  simp only [matchH2, lsbs, msbs]
  bv_decide

theorem h2_only_full (c h : BitVec 64) (hv : (((c >>> 0) &&& 0xFF#64) = 0x80#64 ∨ ((c >>> 0) &&& 0xFF#64) = 0xFE#64 ∨ ((c >>> 0) &&& 0xFF#64) < 0x80#64) ∧ (((c >>> 8) &&& 0xFF#64) = 0x80#64 ∨ ((c >>> 8) &&& 0xFF#64) = 0xFE#64 ∨ ((c >>> 8) &&& 0xFF#64) < 0x80#64) ∧ (((c >>> 16) &&& 0xFF#64) = 0x80#64 ∨ ((c >>> 16) &&& 0xFF#64) = 0xFE#64 ∨ ((c >>> 16) &&& 0xFF#64) < 0x80#64) ∧ (((c >>> 24) &&& 0xFF#64) = 0x80#64 ∨ ((c >>> 24) &&& 0xFF#64) = 0xFE#64 ∨ ((c >>> 24) &&& 0xFF#64) < 0x80#64) ∧ (((c >>> 32) &&& 0xFF#64) = 0x80#64 ∨ ((c >>> 32) &&& 0xFF#64) = 0xFE#64 ∨ ((c >>> 32) &&& 0xFF#64) < 0x80#64) ∧ (((c >>> 40) &&& 0xFF#64) = 0x80#64 ∨ ((c >>> 40) &&& 0xFF#64) = 0xFE#64 ∨ ((c >>> 40) &&& 0xFF#64) < 0x80#64) ∧ (((c >>> 48) &&& 0xFF#64) = 0x80#64 ∨ ((c >>> 48) &&& 0xFF#64) = 0xFE#64 ∨ ((c >>> 48) &&& 0xFF#64) < 0x80#64) ∧ (((c >>> 56) &&& 0xFF#64) = 0x80#64 ∨ ((c >>> 56) &&& 0xFF#64) = 0xFE#64 ∨ ((c >>> 56) &&& 0xFF#64) < 0x80#64)) (hh : h < 0x80#64) :
    (((matchH2 c h >>> 7) &&& 1#64) = 1#64 → ((c >>> 0) &&& 0xFF#64) < 0x80#64) ∧ (((matchH2 c h >>> 15) &&& 1#64) = 1#64 → ((c >>> 8) &&& 0xFF#64) < 0x80#64) ∧ (((matchH2 c h >>> 23) &&& 1#64) = 1#64 → ((c >>> 16) &&& 0xFF#64) < 0x80#64) ∧ (((matchH2 c h >>> 31) &&& 1#64) = 1#64 → ((c >>> 24) &&& 0xFF#64) < 0x80#64) ∧ (((matchH2 c h >>> 39) &&& 1#64) = 1#64 → ((c >>> 32) &&& 0xFF#64) < 0x80#64) ∧ (((matchH2 c h >>> 47) &&& 1#64) = 1#64 → ((c >>> 40) &&& 0xFF#64) < 0x80#64) ∧ (((matchH2 c h >>> 55) &&& 1#64) = 1#64 → ((c >>> 48) &&& 0xFF#64) < 0x80#64) ∧ (((matchH2 c h >>> 63) &&& 1#64) = 1#64 → ((c >>> 56) &&& 0xFF#64) < 0x80#64) := by
  simp only [matchH2, lsbs, msbs]
  bv_decide

theorem in_msbs (c h : BitVec 64) :
    matchH2 c h &&& ~~~msbs = 0#64 ∧ matchEmpty c &&& ~~~msbs = 0#64 ∧ matchFull c &&& ~~~msbs = 0#64 := by
  simp only [matchH2, matchEmpty, matchFull, lsbs, msbs]
  bv_decide

end MapMatch

#print axioms MapMatch.h2_complete
#print axioms MapMatch.h2_only_full
#print axioms MapMatch.empty_exact
