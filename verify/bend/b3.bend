# b3.bend: rule B3 of go-bce (docs/passes.md) on an execution model.
#
# An execution is a trace of block visits, latest first. Each visit records the values, after
# the visit, of the SSA values B3 talks about:
#   i   the block argument of H that the check uses as its index,
#   m   the current value of the check's length n; by E1 this is also the value of the
#       compare's right operand (same SSA value, an equal constant, or an extractvalue at the
#       same position of the same SSA aggregate). It only changes when block d runs: the
#       block that defines n (or the aggregate n is extracted from); d dominates H or is H,
#   nx  the current values of the increments `llvm.add %i, 1`, one per add block in `as`.
# The semantic constraints between consecutive visits (Step) are those of SSA form:
#   - i changes only at a visit of H, to the operand of the edge taken: a non-negative constant
#     (B3 condition 4: an integer constant >= 0, so below half) or one of the increments;
#   - m changes only at a visit of d;
#   - an add block sets its increment to i + 1 modulo 2^w;
#   - H ends in `cond_br %c, ^T, ^F` with T != F, so a visit of T right after a visit of H
#     means the compare (slt or ult of i and m) was true at that visit.
# The theorem: if the CFG facts B3 asks for hold (T's only predecessor is H, T is not entry,
# T dominates the check block x and every add block), then at every visit of x, i < m as
# unsigned words, so the check `ult i, n` cannot fail.
import Base
import ./nat.bend as N
import ./list.bend as L
import ./cfg.bend as C
import ./graph.bend as G
import ./word.bend as W

type Ev is Data:
  Ev{b: Nat, i: Nat, m: Nat, nx: List<&2, Nat>}

def blk(+e: Ev) -> Nat:
  match e:
    case Ev{+b, i, m, nx}:
      b

def blks(tr: +List<Ev>) -> +List<Nat>:
  match tr:
    case Nil{}:
      Nil{}
    case Con{Ev{+b, i, m, nx}, r}:
      Con{b, blks(r)}

def sel(c: Bool, +v: Nat, +x: Nat) -> Nat:
  match c:
    case True{}:
      v
    case False{}:
      x

# the increments after a visit of block b, where v is i + 1 for this visit
def nupd(as: +List<Nat>, xs: +List<Nat>, +b: Nat, +v: Nat) -> +List<Nat>:
  match as xs:
    case Con{a, as2} Con{x, xs2}:
      Con{sel(L.eqn(b, a), v, x), nupd(as2, xs2, b, v)}
    case _ _:
      Nil{}

def AllLT(xs: +List<Nat>, +half: Nat) -> Data:
  match xs:
    case Nil{}:
      Unit
    case Con{x, r}:
      Sigma<&2, &2, N.LT(x, half), _ => AllLT(r, half)>

# i at a visit of H (isH) or not
def IU(isH: Bool, +half: Nat, +pi: Nat, +pnx: +List<Nat>, +ci: Nat) -> Data:
  match isH:
    case True{}:
      Either<&2, &2, N.LT(ci, half), {L.mem(ci, pnx) == True{} : Bool}>
    case False{}:
      {ci == pi : Nat}

def MU(isD: Bool, +pm: Nat, +cm: Nat) -> Data:
  match isD:
    case True{}:
      Unit
    case False{}:
      {cm == pm : Nat}

def BU(isHT: Bool, sgn: Bool, +half: Nat, +pi: Nat, +pm: Nat) -> Data:
  match isHT:
    case True{}:
      {W.cmp(sgn, half, pi, pm) == True{} : Bool}
    case False{}:
      Unit

def Step(+h: Nat, +t: Nat, +d: Nat, +as: +List<Nat>, +half: Nat, +mod: Nat, sgn: Bool, prev: Ev, cur: Ev) -> Data:
  match prev cur:
    case Ev{+pb, +pi, +pm, +pnx} Ev{+cb, +ci, +cm, +cnx}:
      Sigma<&2, &2, IU(L.eqn(h, cb), half, pi, pnx, ci), _ => Sigma<&2, &2, MU(L.eqn(d, cb), pm, cm), _ => Sigma<&2, &2, {cnx == nupd(as, pnx, cb, W.wrap1(mod, ci)) : +List<Nat>}, _ => BU(L.and(L.eqn(h, pb), L.eqn(t, cb)), sgn, half, pi, pm)>>>

# the values at the first visit (before any of them is defined) are taken below half
def Inv(+half: Nat, e: Ev) -> Data:
  match e:
    case Ev{b, +i, m, +nx}:
      Sigma<&2, &2, N.LT(i, half), _ => AllLT(nx, half)>

# TV(cur, rest): the trace cur :: rest is consistent
def TV(+h: Nat, +t: Nat, +d: Nat, +as: +List<Nat>, +half: Nat, +mod: Nat, sgn: Bool, rest: +List<Ev>, +cur: Ev) -> Data:
  match rest:
    case Nil{}:
      Inv(half, cur)
    case Con{+prev, r2}:
      Sigma<&2, &2, Step(h, t, d, as, half, mod, sgn, prev, cur), _ => TV(h, t, d, as, half, mod, sgn, r2, prev)>

def CmpAt(+sgn: Bool, +half: Nat, e: Ev) -> Data:
  match e:
    case Ev{b, +i, +m, nx}:
      {W.cmp(sgn, half, i, m) == True{} : Bool}

law iu_f:
  for c: Bool
  for ec: {False{} == c : Bool}
  for +half: Nat
  for +pi: Nat
  for +pnx: +List<Nat>
  for +ci: Nat
  for iu: IU(c, half, pi, pnx, ci)
  {ci == pi : Nat}

def iu_f(c, ec, half, pi, pnx, ci, iu):
  match c:
    case True{}:
      Empty.absurd({ci == pi : Nat}, L.ft(ec))
    case False{}:
      iu

law mu_f:
  for c: Bool
  for ec: {False{} == c : Bool}
  for +pm: Nat
  for +cm: Nat
  for mu: MU(c, pm, cm)
  {cm == pm : Nat}

def mu_f(c, ec, pm, cm, mu):
  match c:
    case True{}:
      Empty.absurd({cm == pm : Nat}, L.ft(ec))
    case False{}:
      mu

law bu_t:
  for c: Bool
  for ec: {c == True{} : Bool}
  for +sgn: Bool
  for +half: Nat
  for +pi: Nat
  for +pm: Nat
  for bu: BU(c, sgn, half, pi, pm)
  {W.cmp(sgn, half, pi, pm) == True{} : Bool}

def bu_t(c, ec, sgn, half, pi, pm, bu):
  match c:
    case True{}:
      bu
    case False{}:
      Empty.absurd({W.cmp(sgn, half, pi, pm) == True{} : Bool}, L.ft(ec))

law cmp_tr:
  for +sgn: Bool
  for +half: Nat
  for +pi: Nat
  for +pm: Nat
  for +ci: Nat
  for +cm: Nat
  for eci: {ci == pi : Nat}
  for ecm: {cm == pm : Nat}
  for h: {W.cmp(sgn, half, pi, pm) == True{} : Bool}
  {W.cmp(sgn, half, ci, cm) == True{} : Bool}

def cmp_tr(sgn, half, pi, pm, ci, cm, eci, ecm, h):
  %Equal.sym(Nat, ci, pi, eci) : {W.cmp(sgn, half, _, cm) == True{} : Bool}
  %Equal.sym(Nat, cm, pm, ecm) : {W.cmp(sgn, half, pi, _) == True{} : Bool}
  h

def scan_nil(+es: +List<G.Ed>, +entry: Nat, +h: Nat, +t: Nat, +cb: Nat, c: Bool, +ec: {c == L.eqn(h, cb) : Bool}, mt: {L.mem(t, L.before.put(cb, Nil{}, c)) == True{} : Bool}, +pw: {cb == entry : Nat}, +te: {L.eqn(t, entry) == False{} : Bool}) -> Empty:
  match c:
    case True{}:
      L.ft(mt)
    case False{}:
      +tc : {t == cb : Nat} = L.eqn_true(t, cb, L.or_tf(L.eqn(t, cb), False{}, mt, {==}))
      +tent : {t == entry : Nat} = Equal.trans(Nat, t, cb, entry, tc, pw)
      L.bclash(L.eqn(t, entry), (%tent : {L.eqn(t, _) == True{} : Bool}; L.eqn_refl(t)), te)

def scan_t(+es: +List<G.Ed>, +h: Nat, +t: Nat, +sgn: Bool, +half: Nat, +pb: Nat, +pi: Nat, +pm: Nat, +cb: Nat, +ci: Nat, +cm: Nat, +b2: +List<Nat>, c: Bool, +ec: {c == L.eqn(t, cb) : Bool}, +ok: {G.predsok(es, h, t) == True{} : Bool}, +e: {G.edge(es, pb, cb) == True{} : Bool}, +eci: {ci == pi : Nat}, +ecm: {cm == pm : Nat}, bu: BU(L.and(L.eqn(h, pb), L.eqn(t, cb)), sgn, half, pi, pm), mt: {L.or(c, L.mem(t, b2)) == True{} : Bool}, rec: {L.mem(t, b2) == True{} : Bool} -> {W.cmp(sgn, half, pi, pm) == True{} : Bool}) -> {W.cmp(sgn, half, ci, cm) == True{} : Bool}:
  match c:
    case True{}:
      +tcb : {t == cb : Nat} = L.eqn_true(t, cb, Equal.sym(Bool, True{}, L.eqn(t, cb), ec))
      +e2 : {G.edge(es, pb, t) == True{} : Bool} = (%Equal.sym(Nat, t, cb, tcb) : {G.edge(es, pb, _) == True{} : Bool}; e)
      +phb : {pb == h : Nat} = G.sp_pt(es, h, t, ok, pb, e2)
      +ehp : {L.eqn(h, pb) == True{} : Bool} = (%Equal.sym(Nat, pb, h, phb) : {L.eqn(h, _) == True{} : Bool}; L.eqn_refl(h))
      +g : {L.and(L.eqn(h, pb), L.eqn(t, cb)) == True{} : Bool} = L.and_tt(L.eqn(h, pb), L.eqn(t, cb), ehp, Equal.sym(Bool, True{}, L.eqn(t, cb), ec))
      cmp_tr(sgn, half, pi, pm, ci, cm, eci, ecm, bu_t(L.and(L.eqn(h, pb), L.eqn(t, cb)), g, sgn, half, pi, pm, bu))
    case False{}:
      cmp_tr(sgn, half, pi, pm, ci, cm, eci, ecm, rec(mt))

def scan_con(+es: +List<G.Ed>, +h: Nat, +t: Nat, +d: Nat, +sgn: Bool, +half: Nat, +pb: Nat, +pi: Nat, +pm: Nat, +pnx: +List<Nat>, +cb: Nat, +ci: Nat, +cm: Nat, +rb: +List<Nat>, c: Bool, +ec: {c == L.eqn(h, cb) : Bool}, mt: {L.mem(t, L.before.put(cb, L.before(h, Con{pb, rb}), c)) == True{} : Bool}, +nd: {L.mem(d, L.before.put(cb, L.before(h, Con{pb, rb}), c)) == False{} : Bool}, iu: IU(L.eqn(h, cb), half, pi, pnx, ci), mu: MU(L.eqn(d, cb), pm, cm), bu: BU(L.and(L.eqn(h, pb), L.eqn(t, cb)), sgn, half, pi, pm), +e: {G.edge(es, pb, cb) == True{} : Bool}, +ok: {G.predsok(es, h, t) == True{} : Bool}, rec: {L.mem(t, L.before(h, Con{pb, rb})) == True{} : Bool} -> {L.mem(d, L.before(h, Con{pb, rb})) == False{} : Bool} -> {W.cmp(sgn, half, pi, pm) == True{} : Bool}) -> {W.cmp(sgn, half, ci, cm) == True{} : Bool}:
  match c:
    case True{}:
      Empty.absurd({W.cmp(sgn, half, ci, cm) == True{} : Bool}, L.ft(mt))
    case False{}:
      +eci : {ci == pi : Nat} = iu_f(L.eqn(h, cb), ec, half, pi, pnx, ci, iu)
      +ndc : {L.eqn(d, cb) == False{} : Bool} = L.or_false_l(L.eqn(d, cb), L.mem(d, L.before(h, Con{pb, rb})), nd)
      +ecm : {cm == pm : Nat} = mu_f(L.eqn(d, cb), Equal.sym(Bool, L.eqn(d, cb), False{}, ndc), pm, cm, mu)
      +nd2 : {L.mem(d, L.before(h, Con{pb, rb})) == False{} : Bool} = L.or_false_r(L.eqn(d, cb), L.mem(d, L.before(h, Con{pb, rb})), nd)
      scan_t(es, h, t, sgn, half, pb, pi, pm, cb, ci, cm, L.before(h, Con{pb, rb}), L.eqn(t, cb), {==}, ok, e, eci, ecm, bu, mt, mt2 => rec(mt2, nd2))

# Scan: if T occurs after the last H and d does not, the compare is true for the current i, m.
law scan:
  for +es: +List<G.Ed>
  for +entry: Nat
  for +h: Nat
  for +t: Nat
  for +d: Nat
  for +as: +List<Nat>
  for +half: Nat
  for +mod: Nat
  for +sgn: Bool
  for +rest: +List<Ev>
  for +cur: Ev
  for +ok: {G.predsok(es, h, t) == True{} : Bool}
  for +te: {L.eqn(t, entry) == False{} : Bool}
  for +tv: TV(h, t, d, as, half, mod, sgn, rest, cur)
  for +pw: C.VW(G.EE(es), entry, blk(cur), blks(rest))
  for mt: {L.mem(t, L.before(h, Con{blk(cur), blks(rest)})) == True{} : Bool}
  for +nd: {L.mem(d, L.before(h, Con{blk(cur), blks(rest)})) == False{} : Bool}
  CmpAt(sgn, half, cur)

def scan(es, entry, h, t, d, as, half, mod, sgn, rest, cur, ok, te, tv, pw, mt, nd):
  match rest cur:
    case Nil{} Ev{cb, ci, cm, cnx}:
      Empty.absurd({W.cmp(sgn, half, ci, cm) == True{} : Bool}, scan_nil(es, entry, h, t, cb, L.eqn(h, cb), {==}, mt, pw, te))
    case Con{Ev{pb, pi, pm, pnx}, r2} Ev{cb, ci, cm, cnx}:
      (+st, +tv2) = tv
      (+iu, +q1) = st
      (+mu, +q2) = q1
      (+nu, +bu) = q2
      (+e, +pw2) = pw
      scan_con(es, h, t, d, sgn, half, pb, pi, pm, pnx, cb, ci, cm, blks(r2), L.eqn(h, cb), {==}, mt, nd, iu, mu, bu, e, ok, mt2 => nd2 => scan(es, entry, h, t, d, as, half, mod, sgn, r2, Ev{pb, pi, pm, pnx}, ok, te, tv2, pw2, mt2, nd2))
