# b2.bend: rules B1, B2 and N2 of go-bce (docs/passes.md).
#
# B2 model: a trace of block visits, latest first, recording after each visit the current
# values of the check's index i and length n (by E1 the dominating check reads the same
# values: same SSA value, equal constants, or extractvalue at one position of one SSA
# aggregate). i only changes at visits of block di, n only at visits of dn: the blocks that
# define them (or the aggregate they are extracted from). Both dominate k, the block of the
# dominating check (SSA: a definition dominates its uses), or are k. When a visit of k is
# followed by another visit, the check in k passed: execution left k, and a failed check
# panics. The check being removed is in block x != k with k dominating x. (When both checks
# are in the same block the dominating one runs earlier in the same visit with the same
# values, so the case is immediate.)
#
# N2 is B2 with the constant index 0 and the pointer as the length: a nil check of p passes
# exactly when 0 < p as an unsigned word.
import Base
import ./nat.bend as N
import ./list.bend as L
import ./cfg.bend as C
import ./graph.bend as G
import ./word.bend as W

# B1: constants with 0 <= i < n as signed words: then i < n as unsigned words, which is the
# check (`ult i, n`).
law b1:
  for +half: Nat
  for +i: Nat
  for +n: Nat
  for +nonneg: N.LT(i, half)
  for +lt: {W.slt(half, i, n) == True{} : Bool}
  N.LT(i, n)

def b1.first(+half: Nat, +i: Nat, +n: Nat, p: Sigma<&2, &2, N.LT(i, n), _ => N.LT(n, half)>) -> N.LT(i, n):
  (+a, +b) = p
  a

def b1(half, i, n, nonneg, lt):
  b1.first(half, i, n, W.slt_nonneg(half, i, n, lt, nonneg))

type Ev2 is Data:
  Ev2{b: Nat, i: Nat, n: Nat}

def blk2(+e: Ev2) -> Nat:
  match e:
    case Ev2{+b, i, n}:
      b

def blks2(tr: +List<Ev2>) -> +List<Nat>:
  match tr:
    case Nil{}:
      Nil{}
    case Con{Ev2{+b, i, n}, r}:
      Con{b, blks2(r)}

def KeepU(isD: Bool, +p: Nat, +c: Nat) -> Data:
  match isD:
    case True{}:
      Unit
    case False{}:
      {c == p : Nat}

def PassU(isK: Bool, +pi: Nat, +pn: Nat) -> Data:
  match isK:
    case True{}:
      {N.ltb(pi, pn) == True{} : Bool}
    case False{}:
      Unit

def Step2(+k: Nat, +di: Nat, +dn: Nat, prev: Ev2, cur: Ev2) -> Data:
  match prev cur:
    case Ev2{+pb, +pi, +pn} Ev2{+cb, +ci, +cn}:
      Sigma<&2, &2, KeepU(L.eqn(di, cb), pi, ci), _ => Sigma<&2, &2, KeepU(L.eqn(dn, cb), pn, cn), _ => PassU(L.eqn(k, pb), pi, pn)>>

def TV2(+k: Nat, +di: Nat, +dn: Nat, rest: +List<Ev2>, +cur: Ev2) -> Data:
  match rest:
    case Nil{}:
      Unit
    case Con{+prev, r2}:
      Sigma<&2, &2, Step2(k, di, dn, prev, cur), _ => TV2(k, di, dn, r2, prev)>

def LtAt2(e: Ev2) -> Data:
  match e:
    case Ev2{b, +i, +n}:
      {N.ltb(i, n) == True{} : Bool}

law keep_f:
  for c: Bool
  for +ec: {False{} == c : Bool}
  for +p: Nat
  for +x: Nat
  for ku: KeepU(c, p, x)
  {x == p : Nat}

def keep_f(c, ec, p, x, ku):
  match c:
    case True{}:
      Empty.absurd({x == p : Nat}, L.ft(ec))
    case False{}:
      ku

def scan2.prev(+k: Nat, +pb: Nat, c: Bool, +ec: {c == L.eqn(k, pb) : Bool}, +pi: Nat, +pn: Nat, +ci: Nat, +cn: Nat, +eci: {ci == pi : Nat}, +ecn: {cn == pn : Nat}, pu: PassU(c, pi, pn), rec: {L.eqn(k, pb) == False{} : Bool} -> {N.ltb(pi, pn) == True{} : Bool}) -> {N.ltb(ci, cn) == True{} : Bool}:
  match c:
    case True{}:
      %Equal.sym(Nat, ci, pi, eci) : {N.ltb(_, cn) == True{} : Bool}
      %Equal.sym(Nat, cn, pn, ecn) : {N.ltb(pi, _) == True{} : Bool}
      pu
    case False{}:
      %Equal.sym(Nat, ci, pi, eci) : {N.ltb(_, cn) == True{} : Bool}
      %Equal.sym(Nat, cn, pn, ecn) : {N.ltb(pi, _) == True{} : Bool}
      rec(Equal.sym(Bool, False{}, L.eqn(k, pb), ec))

law scan2:
  for +k: Nat
  for +di: Nat
  for +dn: Nat
  for +rest: +List<Ev2>
  for +cur: Ev2
  for +tv: TV2(k, di, dn, rest, cur)
  for +nk: {L.eqn(k, blk2(cur)) == False{} : Bool}
  for +mk: {L.mem(k, Con{blk2(cur), blks2(rest)}) == True{} : Bool}
  for +ndi: {L.mem(di, L.before(k, Con{blk2(cur), blks2(rest)})) == False{} : Bool}
  for +ndn: {L.mem(dn, L.before(k, Con{blk2(cur), blks2(rest)})) == False{} : Bool}
  LtAt2(cur)

def scan2(k, di, dn, rest, cur, tv, nk, mk, ndi, ndn):
  match rest cur:
    case Nil{} Ev2{cb, ci, cn}:
      Empty.absurd({N.ltb(ci, cn) == True{} : Bool}, L.bclash(L.eqn(k, cb), L.or_tf(L.eqn(k, cb), False{}, mk, {==}), nk))
    case Con{Ev2{pb, pi, pn}, r2} Ev2{cb, ci, cn}:
      (+st, +tv2) = tv
      (+kui, +q) = st
      (+kun, +pu) = q
      +ndi1 : {L.mem(di, Con{cb, L.before(k, Con{pb, blks2(r2)})}) == False{} : Bool} = (%nk : {L.mem(di, L.before.put(cb, L.before(k, Con{pb, blks2(r2)}), _)) == False{} : Bool}; ndi)
      +ndn1 : {L.mem(dn, Con{cb, L.before(k, Con{pb, blks2(r2)})}) == False{} : Bool} = (%nk : {L.mem(dn, L.before.put(cb, L.before(k, Con{pb, blks2(r2)}), _)) == False{} : Bool}; ndn)
      +eci : {ci == pi : Nat} = keep_f(L.eqn(di, cb), Equal.sym(Bool, L.eqn(di, cb), False{}, L.or_false_l(L.eqn(di, cb), L.mem(di, L.before(k, Con{pb, blks2(r2)})), ndi1)), pi, ci, kui)
      +ecn : {cn == pn : Nat} = keep_f(L.eqn(dn, cb), Equal.sym(Bool, L.eqn(dn, cb), False{}, L.or_false_l(L.eqn(dn, cb), L.mem(dn, L.before(k, Con{pb, blks2(r2)})), ndn1)), pn, cn, kun)
      +ndi2 : {L.mem(di, L.before(k, Con{pb, blks2(r2)})) == False{} : Bool} = L.or_false_r(L.eqn(di, cb), L.mem(di, L.before(k, Con{pb, blks2(r2)})), ndi1)
      +ndn2 : {L.mem(dn, L.before(k, Con{pb, blks2(r2)})) == False{} : Bool} = L.or_false_r(L.eqn(dn, cb), L.mem(dn, L.before(k, Con{pb, blks2(r2)})), ndn1)
      +mk2 : {L.mem(k, Con{pb, blks2(r2)}) == True{} : Bool} = L.or_fl(L.eqn(k, cb), L.mem(k, Con{pb, blks2(r2)}), Equal.sym(Bool, L.eqn(k, cb), False{}, nk), mk)
      scan2.prev(k, pb, L.eqn(k, pb), {==}, pi, pn, ci, cn, eci, ecn, pu, nkp => scan2(k, di, dn, r2, Ev2{pb, pi, pn}, tv2, nkp, mk2, ndi2, ndn2))

def nd2_at.go(+es: +List<G.Ed>, +entry: Nat, +k: Nat, +dv: Nat, +Sk: +List<Nat>, +Sv: +List<Nat>, +x: Nat, c: Bool, +ec: {c == L.eqn(dv, k) : Bool}, +dc: {L.or(c, G.domc(es, entry, dv, Sv, k)) == True{} : Bool}, +dkx: {G.domc(es, entry, k, Sk, x) == True{} : Bool}, +w: +List<Nat>, +pw: C.Valid(G.EE(es), entry, w), +hw: {L.hd(w) == x : Nat}) -> {L.mem(dv, L.before(k, w)) == False{} : Bool}:
  match c:
    case True{}:
      +dk : {dv == k : Nat} = L.eqn_true(dv, k, Equal.sym(Bool, True{}, L.eqn(dv, k), ec))
      (%Equal.sym(Nat, dv, k, dk) : {L.mem(_, L.before(k, w)) == False{} : Bool}; L.before_no(k, w))
    case False{}:
      C.d_after_k(G.EE(es), entry, w, x, k, dv, Equal.sym(Bool, False{}, L.eqn(dv, k), ec), G.dom_of(es, entry, dv, Sv, k, dc), G.dom_of(es, entry, k, Sk, x, dkx), pw, hw)

# B2. Hypotheses, checkable on the CFG: k dominates x (certificate Sk) and k != x; the block
# defining i (and the one defining n) is k or dominates k (certificates Si, Sn).
# For every consistent execution now at x: i < n as unsigned words.
law b2:
  for +es: +List<G.Ed>
  for +entry: Nat
  for +k: Nat
  for +di: Nat
  for +dn: Nat
  for +x: Nat
  for +Sk: +List<Nat>
  for +Si: +List<Nat>
  for +Sn: +List<Nat>
  for +rest: +List<Ev2>
  for +cur: Ev2
  for +dkx: {G.domc(es, entry, k, Sk, x) == True{} : Bool}
  for +xk: {L.eqn(k, x) == False{} : Bool}
  for +dci: {L.or(L.eqn(di, k), G.domc(es, entry, di, Si, k)) == True{} : Bool}
  for +dcn: {L.or(L.eqn(dn, k), G.domc(es, entry, dn, Sn, k)) == True{} : Bool}
  for +tv: TV2(k, di, dn, rest, cur)
  for +pw: C.VW(G.EE(es), entry, blk2(cur), blks2(rest))
  for +hx: {blk2(cur) == x : Nat}
  LtAt2(cur)

def b2(es, entry, k, di, dn, x, Sk, Si, Sn, rest, cur, dkx, xk, dci, dcn, tv, pw, hx):
  +mk : {L.mem(k, Con{blk2(cur), blks2(rest)}) == True{} : Bool} = G.dom_pt(es, entry, k, Sk, x, dkx, Con{blk2(cur), blks2(rest)}, pw, hx)
  +nk : {L.eqn(k, blk2(cur)) == False{} : Bool} = (%Equal.sym(Nat, blk2(cur), x, hx) : {L.eqn(k, _) == False{} : Bool}; xk)
  +ndi : {L.mem(di, L.before(k, Con{blk2(cur), blks2(rest)})) == False{} : Bool} = nd2_at.go(es, entry, k, di, Sk, Si, x, L.eqn(di, k), {==}, dci, dkx, Con{blk2(cur), blks2(rest)}, pw, hx)
  +ndn : {L.mem(dn, L.before(k, Con{blk2(cur), blks2(rest)})) == False{} : Bool} = nd2_at.go(es, entry, k, dn, Sk, Sn, x, L.eqn(dn, k), {==}, dcn, dkx, Con{blk2(cur), blks2(rest)}, pw, hx)
  scan2(k, di, dn, rest, cur, tv, nk, mk, ndi, ndn)
