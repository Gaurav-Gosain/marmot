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

# T occurs after the last H on a walk at a block T dominates (T1 with checked facts)
law mt_at:
  for +es: +List<G.Ed>
  for +entry: Nat
  for +h: Nat
  for +t: Nat
  for +St: +List<Nat>
  for +x: Nat
  for +ok: {G.predsok(es, h, t) == True{} : Bool}
  for +te: {L.eqn(t, entry) == False{} : Bool}
  for +dx: {G.domc(es, entry, t, St, x) == True{} : Bool}
  for +w: +List<Nat>
  for +pw: C.Valid(G.EE(es), entry, w)
  for +hw: {L.hd(w) == x : Nat}
  {L.mem(t, L.before(h, w)) == True{} : Bool}

def mt_at(es, entry, h, t, St, x, ok, te, dx, w, pw, hw):
  C.t_after_h(G.EE(es), entry, w, x, h, t, G.sp_of(es, h, t, ok), G.sp_of(es, h, t, ok), te, G.dom_of(es, entry, t, St, x, dx), pw, hw)

def nd_at.go(+es: +List<G.Ed>, +entry: Nat, +h: Nat, +t: Nat, +d: Nat, +St: +List<Nat>, +Sd: +List<Nat>, +x: Nat, c: Bool, +ec: {c == L.eqn(d, h) : Bool}, +dc: {L.or(c, G.domc(es, entry, d, Sd, h)) == True{} : Bool}, +ok: {G.predsok(es, h, t) == True{} : Bool}, +te: {L.eqn(t, entry) == False{} : Bool}, +dx: {G.domc(es, entry, t, St, x) == True{} : Bool}, +w: +List<Nat>, +pw: C.Valid(G.EE(es), entry, w), +hw: {L.hd(w) == x : Nat}) -> {L.mem(d, L.before(h, w)) == False{} : Bool}:
  match c:
    case True{}:
      +dh : {d == h : Nat} = L.eqn_true(d, h, Equal.sym(Bool, True{}, L.eqn(d, h), ec))
      (%Equal.sym(Nat, d, h, dh) : {L.mem(_, L.before(h, w)) == False{} : Bool}; L.before_no(h, w))
    case False{}:
      C.d_after_h(G.EE(es), entry, w, x, h, t, d, Equal.sym(Bool, False{}, L.eqn(d, h), ec), G.dom_of(es, entry, d, Sd, h, dc), G.sp_of(es, h, t, ok), G.sp_of(es, h, t, ok), G.sp_of(es, h, t, ok), te, G.dom_of(es, entry, t, St, x, dx), pw, hw)

# the block d that sets m is not visited after the last H (it is H, or T2 applies)
law nd_at:
  for +es: +List<G.Ed>
  for +entry: Nat
  for +h: Nat
  for +t: Nat
  for +d: Nat
  for +St: +List<Nat>
  for +Sd: +List<Nat>
  for +x: Nat
  for +dc: {L.or(L.eqn(d, h), G.domc(es, entry, d, Sd, h)) == True{} : Bool}
  for +ok: {G.predsok(es, h, t) == True{} : Bool}
  for +te: {L.eqn(t, entry) == False{} : Bool}
  for +dx: {G.domc(es, entry, t, St, x) == True{} : Bool}
  for +w: +List<Nat>
  for +pw: C.Valid(G.EE(es), entry, w)
  for +hw: {L.hd(w) == x : Nat}
  {L.mem(d, L.before(h, w)) == False{} : Bool}

def nd_at(es, entry, h, t, d, St, Sd, x, dc, ok, te, dx, w, pw, hw):
  nd_at.go(es, entry, h, t, d, St, Sd, x, L.eqn(d, h), {==}, dc, ok, te, dx, w, pw, hw)

# every add block is dominated by T (one certificate St for all of them)
def alldomc(+es: +List<G.Ed>, +entry: Nat, +t: Nat, +S: +List<Nat>, as: +List<Nat>) -> Bool:
  match as:
    case Nil{}:
      True{}
    case Con{+a, r}:
      L.and(G.domc(es, entry, t, S, a), alldomc(es, entry, t, S, r))

def alldom_mem.go(+es: +List<G.Ed>, +entry: Nat, +t: Nat, +S: +List<Nat>, +a: Nat, +r: +List<Nat>, +b: Nat, c: Bool, +ec: {c == L.eqn(b, a) : Bool}, +ad: {L.and(G.domc(es, entry, t, S, a), alldomc(es, entry, t, S, r)) == True{} : Bool}, m: {L.or(c, L.mem(b, r)) == True{} : Bool}, rec: {alldomc(es, entry, t, S, r) == True{} : Bool} -> {L.mem(b, r) == True{} : Bool} -> {G.domc(es, entry, t, S, b) == True{} : Bool}) -> {G.domc(es, entry, t, S, b) == True{} : Bool}:
  match c:
    case True{}:
      +ba : {b == a : Nat} = L.eqn_true(b, a, Equal.sym(Bool, True{}, L.eqn(b, a), ec))
      (%Equal.sym(Nat, b, a, ba) : {G.domc(es, entry, t, S, _) == True{} : Bool}; L.and_l(G.domc(es, entry, t, S, a), alldomc(es, entry, t, S, r), ad))
    case False{}:
      rec(L.and_r(G.domc(es, entry, t, S, a), alldomc(es, entry, t, S, r), ad), m)

law alldom_mem:
  for +es: +List<G.Ed>
  for +entry: Nat
  for +t: Nat
  for +S: +List<Nat>
  for +as: +List<Nat>
  for +b: Nat
  for +ad: {alldomc(es, entry, t, S, as) == True{} : Bool}
  for m: {L.mem(b, as) == True{} : Bool}
  {G.domc(es, entry, t, S, b) == True{} : Bool}

def alldom_mem(es, entry, t, S, as, b, ad, m):
  match as:
    case Nil{}:
      Empty.absurd({G.domc(es, entry, t, S, b) == True{} : Bool}, L.ft(m))
    case Con{a, r}:
      alldom_mem.go(es, entry, t, S, a, r, b, L.eqn(b, a), {==}, ad, m, ad2 => m2 => alldom_mem(es, entry, t, S, r, b, ad2, m2))

def all_mem.go(+x: Nat, +y: Nat, +r: +List<Nat>, +half: Nat, c: Bool, +ec: {c == L.eqn(x, y) : Bool}, +ly: N.LT(y, half), m: {L.or(c, L.mem(x, r)) == True{} : Bool}, rec: {L.mem(x, r) == True{} : Bool} -> N.LT(x, half)) -> N.LT(x, half):
  match c:
    case True{}:
      +xy : {x == y : Nat} = L.eqn_true(x, y, Equal.sym(Bool, True{}, L.eqn(x, y), ec))
      (%Equal.sym(Nat, x, y, xy) : N.LT(_, half); ly)
    case False{}:
      rec(m)

law all_mem:
  for +xs: +List<Nat>
  for +x: Nat
  for +half: Nat
  for +ax: AllLT(xs, half)
  for m: {L.mem(x, xs) == True{} : Bool}
  N.LT(x, half)

def all_mem(xs, x, half, ax, m):
  match xs:
    case Nil{}:
      Empty.absurd(N.LT(x, half), L.ft(m))
    case Con{y, r}:
      (+ly, +ar) = ax
      all_mem.go(x, y, r, half, L.eqn(x, y), {==}, ly, m, m2 => all_mem(r, x, half, ar, m2))

law sel_lt:
  for c: Bool
  for +v: Nat
  for +x: Nat
  for +half: Nat
  for +lv: N.LT(v, half)
  for +lx: N.LT(x, half)
  N.LT(sel(c, v, x), half)

def sel_lt(c, v, x, half, lv, lx):
  match c:
    case True{}:
      lv
    case False{}:
      lx

law sel_lt_f:
  for c: Bool
  for +ec: {c == False{} : Bool}
  for +v: Nat
  for +x: Nat
  for +half: Nat
  for +lx: N.LT(x, half)
  N.LT(sel(c, v, x), half)

def sel_lt_f(c, ec, v, x, half, lx):
  match c:
    case True{}:
      Empty.absurd(N.LT(v, half), L.tf(ec))
    case False{}:
      lx

law nupd_yes:
  for +as: +List<Nat>
  for +xs: +List<Nat>
  for +b: Nat
  for +v: Nat
  for +half: Nat
  for +lv: N.LT(v, half)
  for +ax: AllLT(xs, half)
  AllLT(nupd(as, xs, b, v), half)

def nupd_yes(as, xs, b, v, half, lv, ax):
  match as:
    case Nil{}:
      Unit{}
    case Con{a, as2}:
      match xs:
        case Nil{}:
          Unit{}
        case Con{x, xs2}:
          (+lx, +ax2) = ax
          (sel_lt(L.eqn(b, a), v, x, half, lv, lx), nupd_yes(as2, xs2, b, v, half, lv, ax2))

law nupd_no:
  for +as: +List<Nat>
  for +xs: +List<Nat>
  for +b: Nat
  for +v: Nat
  for +half: Nat
  for +nm: {L.mem(b, as) == False{} : Bool}
  for +ax: AllLT(xs, half)
  AllLT(nupd(as, xs, b, v), half)

def nupd_no(as, xs, b, v, half, nm, ax):
  match as:
    case Nil{}:
      Unit{}
    case Con{a, as2}:
      match xs:
        case Nil{}:
          Unit{}
        case Con{x, xs2}:
          (+lx, +ax2) = ax
          +nba : {L.eqn(b, a) == False{} : Bool} = L.or_false_l(L.eqn(b, a), L.mem(b, as2), nm)
          +nm2 : {L.mem(b, as2) == False{} : Bool} = L.or_false_r(L.eqn(b, a), L.mem(b, as2), nm)
          (sel_lt_f(L.eqn(b, a), nba, v, x, half, lx), nupd_no(as2, xs2, b, v, half, nm2, ax2))

def inv_i(c: Bool, +half: Nat, +pi: Nat, +pnx: +List<Nat>, +ci: Nat, iu: IU(c, half, pi, pnx, ci), +lpi: N.LT(pi, half), +lpnx: AllLT(pnx, half)) -> N.LT(ci, half):
  match c:
    case True{}:
      match iu:
        case Inl{l}:
          l
        case Inr{m}:
          all_mem(pnx, ci, half, lpnx, m)
    case False{}:
      (%Equal.sym(Nat, ci, pi, iu) : N.LT(_, half); lpi)

def lw_fin(+mod: Nat, +half: Nat, +ci: Nat, +cm: Nat, +hm: N.LE(half, mod), sn: Sigma<&2, &2, N.LT(ci, cm), _ => N.LT(cm, half)>) -> N.LT(W.wrap1(mod, ci), half):
  (+l1, +l2) = sn
  +l3 : N.LT(1n+ci, half) = N.le_lt_trans(1n+ci, cm, half, l1, l2)
  +l4 : N.LT(1n+ci, mod) = N.lt_le_trans(1n+ci, half, mod, l3, hm)
  +w : {W.wrap1(mod, ci) == 1n+ci : Nat} = W.wrap1_small(mod, ci, l4)
  (%Equal.sym(Nat, W.wrap1(mod, ci), 1n+ci, w) : N.LT(_, half); l3)

# at a visit of an add block (signed compare, i non-negative): i + 1 stays below half
law lw_of:
  for +es: +List<G.Ed>
  for +entry: Nat
  for +h: Nat
  for +t: Nat
  for +d: Nat
  for +as: +List<Nat>
  for +half: Nat
  for +mod: Nat
  for +St: +List<Nat>
  for +Sd: +List<Nat>
  for +rest: +List<Ev>
  for +cb: Nat
  for +ci: Nat
  for +cm: Nat
  for +cnx: +List<Nat>
  for +ok: {G.predsok(es, h, t) == True{} : Bool}
  for +te: {L.eqn(t, entry) == False{} : Bool}
  for +dc: {L.or(L.eqn(d, h), G.domc(es, entry, d, Sd, h)) == True{} : Bool}
  for +ad: {alldomc(es, entry, t, St, as) == True{} : Bool}
  for +hm: N.LE(half, mod)
  for +tv: TV(h, t, d, as, half, mod, True{}, rest, Ev{cb, ci, cm, cnx})
  for +pw: C.VW(G.EE(es), entry, cb, blks(rest))
  for +lci: N.LT(ci, half)
  for +m: {L.mem(cb, as) == True{} : Bool}
  N.LT(W.wrap1(mod, ci), half)

def lw_of(es, entry, h, t, d, as, half, mod, St, Sd, rest, cb, ci, cm, cnx, ok, te, dc, ad, hm, tv, pw, lci, m):
  +dx : {G.domc(es, entry, t, St, cb) == True{} : Bool} = alldom_mem(es, entry, t, St, as, cb, ad, m)
  +mt : {L.mem(t, L.before(h, Con{cb, blks(rest)})) == True{} : Bool} = mt_at(es, entry, h, t, St, cb, ok, te, dx, Con{cb, blks(rest)}, pw, {==})
  +nd : {L.mem(d, L.before(h, Con{cb, blks(rest)})) == False{} : Bool} = nd_at(es, entry, h, t, d, St, Sd, cb, dc, ok, te, dx, Con{cb, blks(rest)}, pw, {==})
  +sc : {W.slt(half, ci, cm) == True{} : Bool} = scan(es, entry, h, t, d, as, half, mod, True{}, rest, Ev{cb, ci, cm, cnx}, ok, te, tv, pw, mt, nd)
  lw_fin(mod, half, ci, cm, hm, W.slt_nonneg(half, ci, cm, sc, lci))

def inv_nx(+as: +List<Nat>, +pnx: +List<Nat>, +cb: Nat, +v: Nat, +half: Nat, c: Bool, +ec: {c == L.mem(cb, as) : Bool}, +lpnx: AllLT(pnx, half), k: {L.mem(cb, as) == True{} : Bool} -> N.LT(v, half)) -> AllLT(nupd(as, pnx, cb, v), half):
  match c:
    case True{}:
      nupd_yes(as, pnx, cb, v, half, k(Equal.sym(Bool, True{}, L.mem(cb, as), ec)), lpnx)
    case False{}:
      nupd_no(as, pnx, cb, v, half, Equal.sym(Bool, False{}, L.mem(cb, as), ec), lpnx)

def inv.fin(+es: +List<G.Ed>, +entry: Nat, +h: Nat, +t: Nat, +d: Nat, +as: +List<Nat>, +half: Nat, +mod: Nat, +St: +List<Nat>, +Sd: +List<Nat>, +pb: Nat, +pi: Nat, +pm: Nat, +pnx: +List<Nat>, +r2: +List<Ev>, +cb: Nat, +ci: Nat, +cm: Nat, +cnx: +List<Nat>, +ok: {G.predsok(es, h, t) == True{} : Bool}, +te: {L.eqn(t, entry) == False{} : Bool}, +dc: {L.or(L.eqn(d, h), G.domc(es, entry, d, Sd, h)) == True{} : Bool}, +ad: {alldomc(es, entry, t, St, as) == True{} : Bool}, +hm: N.LE(half, mod), +tv: TV(h, t, d, as, half, mod, True{}, Con{Ev{pb, pi, pm, pnx}, r2}, Ev{cb, ci, cm, cnx}), +pw: C.VW(G.EE(es), entry, cb, blks(Con{Ev{pb, pi, pm, pnx}, r2})), +iu: IU(L.eqn(h, cb), half, pi, pnx, ci), +nu: {cnx == nupd(as, pnx, cb, W.wrap1(mod, ci)) : +List<Nat>}, +ip: Inv(half, Ev{pb, pi, pm, pnx})) -> Inv(half, Ev{cb, ci, cm, cnx}):
  (+lpi, +lpnx) = ip
  +lci : N.LT(ci, half) = inv_i(L.eqn(h, cb), half, pi, pnx, ci, iu, lpi, lpnx)
  +ln : AllLT(nupd(as, pnx, cb, W.wrap1(mod, ci)), half) = inv_nx(as, pnx, cb, W.wrap1(mod, ci), half, L.mem(cb, as), {==}, lpnx, m => lw_of(es, entry, h, t, d, as, half, mod, St, Sd, Con{Ev{pb, pi, pm, pnx}, r2}, cb, ci, cm, cnx, ok, te, dc, ad, hm, tv, pw, lci, m))
  (lci, (%Equal.sym(+List<Nat>, cnx, nupd(as, pnx, cb, W.wrap1(mod, ci)), nu) : AllLT(_, half); ln))

# INV (signed compare): at every visit, i and every increment are non-negative words.
law inv:
  for +es: +List<G.Ed>
  for +entry: Nat
  for +h: Nat
  for +t: Nat
  for +d: Nat
  for +as: +List<Nat>
  for +half: Nat
  for +mod: Nat
  for +St: +List<Nat>
  for +Sd: +List<Nat>
  for +rest: +List<Ev>
  for +cur: Ev
  for +ok: {G.predsok(es, h, t) == True{} : Bool}
  for +te: {L.eqn(t, entry) == False{} : Bool}
  for +dc: {L.or(L.eqn(d, h), G.domc(es, entry, d, Sd, h)) == True{} : Bool}
  for +ad: {alldomc(es, entry, t, St, as) == True{} : Bool}
  for +hm: N.LE(half, mod)
  for +tv: TV(h, t, d, as, half, mod, True{}, rest, cur)
  for +pw: C.VW(G.EE(es), entry, blk(cur), blks(rest))
  Inv(half, cur)

def inv(es, entry, h, t, d, as, half, mod, St, Sd, rest, cur, ok, te, dc, ad, hm, tv, pw):
  match rest cur:
    case Nil{} Ev{cb, ci, cm, cnx}:
      tv
    case Con{Ev{pb, pi, pm, pnx}, r2} Ev{cb, ci, cm, cnx}:
      (+st, +tv2) = tv
      (+iu, +q1) = st
      (+mu, +q2) = q1
      (+nu, +bu) = q2
      (+e, +pw2) = pw
      +ip : Inv(half, Ev{pb, pi, pm, pnx}) = inv(es, entry, h, t, d, as, half, mod, St, Sd, r2, Ev{pb, pi, pm, pnx}, ok, te, dc, ad, hm, tv2, pw2)
      inv.fin(es, entry, h, t, d, as, half, mod, St, Sd, pb, pi, pm, pnx, r2, cb, ci, cm, cnx, ok, te, dc, ad, hm, (st, tv2), (e, pw2), iu, nu, ip)

def b3.first(+half: Nat, +ci: Nat, +cm: Nat, p: Sigma<&2, &2, N.LT(ci, cm), _ => N.LT(cm, half)>) -> N.LT(ci, cm):
  (+a, +b) = p
  a

def b3.signed(+half: Nat, +cb: Nat, +ci: Nat, +cm: Nat, +cnx: +List<Nat>, +sc: {W.slt(half, ci, cm) == True{} : Bool}, ip: Inv(half, Ev{cb, ci, cm, cnx})) -> N.LT(ci, cm):
  (+lci, +lnx) = ip
  b3.first(half, ci, cm, W.slt_nonneg(half, ci, cm, sc, lci))

def b3.fin(+sgn: Bool, +es: +List<G.Ed>, +entry: Nat, +h: Nat, +t: Nat, +d: Nat, +as: +List<Nat>, +half: Nat, +mod: Nat, +St: +List<Nat>, +Sd: +List<Nat>, +rest: +List<Ev>, +cb: Nat, +ci: Nat, +cm: Nat, +cnx: +List<Nat>, +ok: {G.predsok(es, h, t) == True{} : Bool}, +te: {L.eqn(t, entry) == False{} : Bool}, +dc: {L.or(L.eqn(d, h), G.domc(es, entry, d, Sd, h)) == True{} : Bool}, +ad: {alldomc(es, entry, t, St, as) == True{} : Bool}, +hm: N.LE(half, mod), +tv: TV(h, t, d, as, half, mod, sgn, rest, Ev{cb, ci, cm, cnx}), +pw: C.VW(G.EE(es), entry, cb, blks(rest)), +sc: {W.cmp(sgn, half, ci, cm) == True{} : Bool}) -> N.LT(ci, cm):
  match sgn:
    case True{}:
      b3.signed(half, cb, ci, cm, cnx, sc, inv(es, entry, h, t, d, as, half, mod, St, Sd, rest, Ev{cb, ci, cm, cnx}, ok, te, dc, ad, hm, tv, pw))
    case False{}:
      N.ltb_lt(ci, cm, sc)

def LtAt(e: Ev) -> Data:
  match e:
    case Ev{b, +i, +m, nx}:
      N.LT(i, m)

# B3. Hypotheses, all checkable on the CFG:
#   ok: every edge into t comes from h (t's only predecessor is h);
#   te: t is not the entry block;
#   dc: d (the block that sets the length) is h, or dominates h (certificate Sd);
#   ad: t dominates every add block (certificate St);
#   dx: t dominates the check block x (certificate St);
#   hm: half <= mod (words: half = 2^(w-1), mod = 2^w).
# For every consistent execution (tv, pw) now at x: i < m as unsigned words.
law b3:
  for +es: +List<G.Ed>
  for +entry: Nat
  for +h: Nat
  for +t: Nat
  for +d: Nat
  for +as: +List<Nat>
  for +half: Nat
  for +mod: Nat
  for +St: +List<Nat>
  for +Sd: +List<Nat>
  for +x: Nat
  for +rest: +List<Ev>
  for +cur: Ev
  for +sgn: Bool
  for +ok: {G.predsok(es, h, t) == True{} : Bool}
  for +te: {L.eqn(t, entry) == False{} : Bool}
  for +dc: {L.or(L.eqn(d, h), G.domc(es, entry, d, Sd, h)) == True{} : Bool}
  for +ad: {alldomc(es, entry, t, St, as) == True{} : Bool}
  for +dx: {G.domc(es, entry, t, St, x) == True{} : Bool}
  for +hm: N.LE(half, mod)
  for +tv: TV(h, t, d, as, half, mod, sgn, rest, cur)
  for +pw: C.VW(G.EE(es), entry, blk(cur), blks(rest))
  for +hx: {blk(cur) == x : Nat}
  LtAt(cur)

def b3(es, entry, h, t, d, as, half, mod, St, Sd, x, rest, cur, sgn, ok, te, dc, ad, dx, hm, tv, pw, hx):
  match cur:
    case Ev{cb, ci, cm, cnx}:
      +mt : {L.mem(t, L.before(h, Con{cb, blks(rest)})) == True{} : Bool} = mt_at(es, entry, h, t, St, x, ok, te, dx, Con{cb, blks(rest)}, pw, hx)
      +nd : {L.mem(d, L.before(h, Con{cb, blks(rest)})) == False{} : Bool} = nd_at(es, entry, h, t, d, St, Sd, x, dc, ok, te, dx, Con{cb, blks(rest)}, pw, hx)
      +sc : {W.cmp(sgn, half, ci, cm) == True{} : Bool} = scan(es, entry, h, t, d, as, half, mod, sgn, rest, Ev{cb, ci, cm, cnx}, ok, te, tv, pw, mt, nd)
      b3.fin(sgn, es, entry, h, t, d, as, half, mod, St, Sd, rest, cb, ci, cm, cnx, ok, te, dc, ad, hm, tv, pw, sc)
