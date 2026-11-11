# graph.bend: a concrete CFG (a list of edges) and checkable forms of the facts rule B3 and
# B2 assume: "h is the only predecessor of t" and "t dominates x". Each check is a Bool
# computation; its soundness lemma turns a passed check into the property cfg.bend uses, as
# often as needed.
#
# Dominance certificate: a set S of blocks that contains entry, is closed under edges whose
# target is not t, and does not contain x. Every walk from entry that avoids t stays in S, so
# a walk at x must pass t. Such an S exists exactly when t dominates x and t is not entry
# (take S = the blocks reachable from entry without passing t), so a checker can always
# produce one; only the direction used here is proved.
import Base
import ./list.bend as L
import ./cfg.bend as C

type Ed is Data:
  Ed{a: Nat, b: Nat}

def edge(es: +List<Ed>, +y: Nat, +x: Nat) -> Bool:
  match es:
    case Nil{}:
      False{}
    case Con{Ed{+a, +b}, r}:
      L.or(L.and(L.eqn(y, a), L.eqn(x, b)), edge(r, y, x))

def EE(es: +List<Ed>, y: Nat, x: Nat) -> Data:
  {edge(es, y, x) == True{} : Bool}

# every edge into t comes from h
def predsok(es: +List<Ed>, +h: Nat, +t: Nat) -> Bool:
  match es:
    case Nil{}:
      True{}
    case Con{Ed{+a, +b}, r}:
      L.and(L.imp(L.eqn(b, t), L.eqn(a, h)), predsok(r, h, t))

def sp_pt.go(+es: +List<Ed>, +a: Nat, +b: Nat, +h: Nat, +t: Nat, +y: Nat, c: Bool, ec: {c == L.and(L.eqn(y, a), L.eqn(t, b)) : Bool}, +ok: {L.and(L.imp(L.eqn(b, t), L.eqn(a, h)), predsok(es, h, t)) == True{} : Bool}, e: {L.or(c, edge(es, y, t)) == True{} : Bool}, rec: {predsok(es, h, t) == True{} : Bool} -> {edge(es, y, t) == True{} : Bool} -> {y == h : Nat}) -> {y == h : Nat}:
  match c:
    case True{}:
      +c2 : {L.and(L.eqn(y, a), L.eqn(t, b)) == True{} : Bool} = Equal.sym(Bool, True{}, L.and(L.eqn(y, a), L.eqn(t, b)), ec)
      +ya : {y == a : Nat} = L.eqn_true(y, a, L.and_l(L.eqn(y, a), L.eqn(t, b), c2))
      +tb : {L.eqn(t, b) == True{} : Bool} = L.and_r(L.eqn(y, a), L.eqn(t, b), c2)
      +bt : {L.eqn(b, t) == True{} : Bool} = (%L.eqn_sym(t, b) : {_ == True{} : Bool}; tb)
      +ah : {a == h : Nat} = L.eqn_true(a, h, L.imp_elim(L.eqn(b, t), L.eqn(a, h), L.and_l(L.imp(L.eqn(b, t), L.eqn(a, h)), predsok(es, h, t), ok), bt))
      Equal.trans(Nat, y, a, h, ya, ah)
    case False{}:
      rec(L.and_r(L.imp(L.eqn(b, t), L.eqn(a, h)), predsok(es, h, t), ok), e)

law sp_pt:
  for +es: +List<Ed>
  for +h: Nat
  for +t: Nat
  for +ok: {predsok(es, h, t) == True{} : Bool}
  for +y: Nat
  for e: {edge(es, y, t) == True{} : Bool}
  {y == h : Nat}

def sp_pt(es, h, t, ok, y, e):
  match es:
    case Nil{}:
      Empty.absurd({y == h : Nat}, L.ft(e))
    case Con{Ed{a, b}, r}:
      sp_pt.go(r, a, b, h, t, y, L.and(L.eqn(y, a), L.eqn(t, b)), {==}, ok, e, ok2 => e2 => sp_pt(r, h, t, ok2, y, e2))

def sp_of(+es: +List<Ed>, +h: Nat, +t: Nat, +ok: {predsok(es, h, t) == True{} : Bool}) -> C.SP(EE(es), h, t):
  y => e => sp_pt(es, h, t, ok, y, e)

# every edge a -> b with a in S and b != t has b in S
def closed(es: +List<Ed>, +t: Nat, +S: +List<Nat>) -> Bool:
  match es:
    case Nil{}:
      True{}
    case Con{Ed{+a, +b}, r}:
      L.and(L.imp(L.and(L.mem(a, S), L.not(L.eqn(b, t))), L.mem(b, S)), closed(r, t, S))

def closed_edge.go(+es: +List<Ed>, +a: Nat, +b: Nat, +t: Nat, +S: +List<Nat>, +z: Nat, +y: Nat, c: Bool, ec: {c == L.and(L.eqn(z, a), L.eqn(y, b)) : Bool}, +cl: {L.and(L.imp(L.and(L.mem(a, S), L.not(L.eqn(b, t))), L.mem(b, S)), closed(es, t, S)) == True{} : Bool}, e: {L.or(c, edge(es, z, y)) == True{} : Bool}, +zs: {L.mem(z, S) == True{} : Bool}, +nyt: {L.eqn(y, t) == False{} : Bool}, rec: {closed(es, t, S) == True{} : Bool} -> {edge(es, z, y) == True{} : Bool} -> {L.mem(y, S) == True{} : Bool}) -> {L.mem(y, S) == True{} : Bool}:
  match c:
    case True{}:
      +c2 : {L.and(L.eqn(z, a), L.eqn(y, b)) == True{} : Bool} = Equal.sym(Bool, True{}, L.and(L.eqn(z, a), L.eqn(y, b)), ec)
      +za : {z == a : Nat} = L.eqn_true(z, a, L.and_l(L.eqn(z, a), L.eqn(y, b), c2))
      +yb : {y == b : Nat} = L.eqn_true(y, b, L.and_r(L.eqn(z, a), L.eqn(y, b), c2))
      +as : {L.mem(a, S) == True{} : Bool} = (%za : {L.mem(_, S) == True{} : Bool}; zs)
      +nbt : {L.eqn(b, t) == False{} : Bool} = (%yb : {L.eqn(_, t) == False{} : Bool}; nyt)
      +pre : {L.and(L.mem(a, S), L.not(L.eqn(b, t))) == True{} : Bool} = L.and_tt(L.mem(a, S), L.not(L.eqn(b, t)), as, L.not_false(L.eqn(b, t), nbt))
      +bs : {L.mem(b, S) == True{} : Bool} = L.imp_elim(L.and(L.mem(a, S), L.not(L.eqn(b, t))), L.mem(b, S), L.and_l(L.imp(L.and(L.mem(a, S), L.not(L.eqn(b, t))), L.mem(b, S)), closed(es, t, S), cl), pre)
      (%Equal.sym(Nat, y, b, yb) : {L.mem(_, S) == True{} : Bool}; bs)
    case False{}:
      rec(L.and_r(L.imp(L.and(L.mem(a, S), L.not(L.eqn(b, t))), L.mem(b, S)), closed(es, t, S), cl), e)

law closed_edge:
  for +es: +List<Ed>
  for +t: Nat
  for +S: +List<Nat>
  for +z: Nat
  for +y: Nat
  for +cl: {closed(es, t, S) == True{} : Bool}
  for e: {edge(es, z, y) == True{} : Bool}
  for +zs: {L.mem(z, S) == True{} : Bool}
  for +nyt: {L.eqn(y, t) == False{} : Bool}
  {L.mem(y, S) == True{} : Bool}

def closed_edge(es, t, S, z, y, cl, e, zs, nyt):
  match es:
    case Nil{}:
      Empty.absurd({L.mem(y, S) == True{} : Bool}, L.ft(e))
    case Con{Ed{a, b}, r}:
      closed_edge.go(r, a, b, t, S, z, y, L.and(L.eqn(z, a), L.eqn(y, b)), {==}, cl, e, zs, nyt, cl2 => e2 => closed_edge(r, t, S, z, y, cl2, e2, zs, nyt))

# a walk from entry that avoids t stays in S
law in_s:
  for +es: +List<Ed>
  for +entry: Nat
  for +r: +List<Nat>
  for +y: Nat
  for +t: Nat
  for +S: +List<Nat>
  for +p: C.VW(EE(es), entry, y, r)
  for +nt: {L.mem(t, Con{y, r}) == False{} : Bool}
  for +cl: {closed(es, t, S) == True{} : Bool}
  for +es0: {L.mem(entry, S) == True{} : Bool}
  {L.mem(y, S) == True{} : Bool}

def in_s(es, entry, r, y, t, S, p, nt, cl, es0):
  match r:
    case Nil{}:
      (%Equal.sym(Nat, y, entry, p) : {L.mem(_, S) == True{} : Bool}; es0)
    case Con{z, r2}:
      (+e, +k) = p
      +nty : {L.eqn(t, y) == False{} : Bool} = L.or_false_l(L.eqn(t, y), L.mem(t, Con{z, r2}), nt)
      +nt2 : {L.mem(t, Con{z, r2}) == False{} : Bool} = L.or_false_r(L.eqn(t, y), L.mem(t, Con{z, r2}), nt)
      +nyt : {L.eqn(y, t) == False{} : Bool} = (%L.eqn_sym(t, y) : {_ == False{} : Bool}; nty)
      closed_edge(es, t, S, z, y, cl, e, in_s(es, entry, r2, z, t, S, k, nt2, cl, es0), nyt)

# a walk contains entry
law has_entry:
  for -E: Nat -> Nat -> Data
  for +entry: Nat
  for +r: +List<Nat>
  for +y: Nat
  for +p: C.VW(E, entry, y, r)
  {L.mem(entry, Con{y, r}) == True{} : Bool}

def has_entry(E, entry, r, y, p):
  match r:
    case Nil{}:
      L.mem_hit(entry, y, Nil{}, p)
    case Con{z, r2}:
      (+e, +k) = p
      L.mem_tail(entry, y, Con{z, r2}, has_entry(E, entry, r2, z, k))

# the dominance check: t is entry, or S is a certificate
def domc(es: +List<Ed>, +entry: Nat, +t: Nat, +S: +List<Nat>, +x: Nat) -> Bool:
  L.or(L.eqn(t, entry), L.and(closed(es, t, S), L.and(L.mem(entry, S), L.not(L.mem(x, S)))))

def dom_pt.cert(+es: +List<Ed>, +entry: Nat, +t: Nat, +S: +List<Nat>, +x: Nat, +y: Nat, +r: +List<Nat>, c: Bool, ec: {c == L.mem(t, Con{y, r}) : Bool}, +ok: {L.and(closed(es, t, S), L.and(L.mem(entry, S), L.not(L.mem(x, S)))) == True{} : Bool}, +p: C.VW(EE(es), entry, y, r), +hw: {y == x : Nat}) -> {c == True{} : Bool}:
  match c:
    case True{}:
      {==}
    case False{}:
      +cl : {closed(es, t, S) == True{} : Bool} = L.and_l(closed(es, t, S), L.and(L.mem(entry, S), L.not(L.mem(x, S))), ok)
      +rest : {L.and(L.mem(entry, S), L.not(L.mem(x, S))) == True{} : Bool} = L.and_r(closed(es, t, S), L.and(L.mem(entry, S), L.not(L.mem(x, S))), ok)
      +ys : {L.mem(y, S) == True{} : Bool} = in_s(es, entry, r, y, t, S, p, Equal.sym(Bool, False{}, L.mem(t, Con{y, r}), ec), cl, L.and_l(L.mem(entry, S), L.not(L.mem(x, S)), rest))
      +xs : {L.mem(x, S) == True{} : Bool} = (%hw : {L.mem(_, S) == True{} : Bool}; ys)
      Empty.absurd({False{} == True{} : Bool}, L.bclash(L.mem(x, S), xs, L.not_true(L.mem(x, S), L.and_r(L.mem(entry, S), L.not(L.mem(x, S)), rest))))

def dom_pt.go(+es: +List<Ed>, +entry: Nat, +t: Nat, +S: +List<Nat>, +x: Nat, +y: Nat, +r: +List<Nat>, c: Bool, ec: {c == L.eqn(t, entry) : Bool}, +ok: {L.or(c, L.and(closed(es, t, S), L.and(L.mem(entry, S), L.not(L.mem(x, S))))) == True{} : Bool}, +p: C.VW(EE(es), entry, y, r), +hw: {y == x : Nat}) -> {L.mem(t, Con{y, r}) == True{} : Bool}:
  match c:
    case True{}:
      +te : {t == entry : Nat} = L.eqn_true(t, entry, Equal.sym(Bool, True{}, L.eqn(t, entry), ec))
      (%Equal.sym(Nat, t, entry, te) : {L.mem(_, Con{y, r}) == True{} : Bool}; has_entry(EE(es), entry, r, y, p))
    case False{}:
      dom_pt.cert(es, entry, t, S, x, y, r, L.mem(t, Con{y, r}), {==}, ok, p, hw)

law dom_pt:
  for +es: +List<Ed>
  for +entry: Nat
  for +t: Nat
  for +S: +List<Nat>
  for +x: Nat
  for +ok: {domc(es, entry, t, S, x) == True{} : Bool}
  for +w: +List<Nat>
  for +pw: C.Valid(EE(es), entry, w)
  for +hw: {L.hd(w) == x : Nat}
  {L.mem(t, w) == True{} : Bool}

def dom_pt(es, entry, t, S, x, ok, w, pw, hw):
  match w:
    case Nil{}:
      Empty.absurd({L.mem(t, Nil{}) == True{} : Bool}, pw)
    case Con{y, r}:
      dom_pt.go(es, entry, t, S, x, y, r, L.eqn(t, entry), {==}, ok, pw, hw)

def dom_of(+es: +List<Ed>, +entry: Nat, +t: Nat, +S: +List<Nat>, +x: Nat, +ok: {domc(es, entry, t, S, x) == True{} : Bool}) -> C.Dom(EE(es), entry, t, x):
  w => pw => hw => dom_pt(es, entry, t, S, x, ok, w, pw, hw)
