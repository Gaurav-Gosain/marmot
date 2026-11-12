# sink.bend: allocation sinking (internal/codegen/allocsink.go).
#
# An allocation in block b moves to the start of d, the nearest common dominator of its uses
# (a phi use counts at the end of its predecessor), when d != b and d lies on no cycle that
# avoids b. Two facts make that invisible:
#
# 1. sink_latest (below): b dominates d (d is dominated by b because b dominates every use),
#    d dominates every use u, d != b. Then on every walk now at u, b does not occur after the
#    last d. So the object a use sees after sinking (made at the latest d) belongs to the
#    same execution of b as the object it saw before (made at the latest b).
# 2. At most one d per b: every closed walk from d back to d passes b (the "no cycle that
#    avoids b" condition, checked by inLoopWithout), so between two executions of d there is
#    an execution of b, and sinking never makes two objects where there was one. This is the
#    pass's condition itself, stated here, not derived.
#
# Paths on which no use runs never reach d and allocate nothing; allocation has no other
# observable effect (it cannot panic for the kinds the pass sinks).
import Base
import ./list.bend as L
import ./cfg.bend as C

law sink_latest:
  for -E: Nat -> Nat -> Data
  for +entry: Nat
  for +w: +List<Nat>
  for +u: Nat
  for +d: Nat
  for +b: Nat
  for +bd: {L.eqn(b, d) == False{} : Bool}
  for dombd: C.Dom(E, entry, b, d)
  for domdu: C.Dom(E, entry, d, u)
  for +pw: C.Valid(E, entry, w)
  for +hw: {L.hd(w) == u : Nat}
  {L.mem(b, L.before(d, w)) == False{} : Bool}

def sink_latest(E, entry, w, u, d, b, bd, dombd, domdu, pw, hw):
  C.d_after_k(E, entry, w, u, d, b, bd, dombd, domdu, pw, hw)
