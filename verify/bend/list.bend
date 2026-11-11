# list.bend: Nat equality as a Bool, membership, and the "before the first x" prefix of a list.
import Base

def bdisc(b: Bool) -> Type:
  match b:
    case True{}:
      Empty
    case False{}:
      Unit

law ft:
  for e: {False{} == True{} : Bool}
  Empty

def ft(e):
  %e : bdisc(_)
  Unit{}


def eqn(a: Nat, b: Nat) -> Bool:
  match a b:
    case 0n 0n:
      True{}
    case 0n 1n+y:
      False{}
    case 1n+x 0n:
      False{}
    case 1n+x 1n+y:
      eqn(x, y)

law eqn_refl:
  for +a: Nat
  {eqn(a, a) == True{} : Bool}

def eqn_refl(a):
  match a:
    case 0n:
      {==}
    case 1n+x:
      eqn_refl(x)

law eqn_true:
  for +a: Nat
  for +b: Nat
  for e: {eqn(a, b) == True{} : Bool}
  {a == b : Nat}

def eqn_true(a, b, e):
  match a b:
    case 0n 0n:
      {==}
    case 0n 1n+y:
      Empty.absurd({0n == 1n+y : Nat}, ft(e))
    case 1n+x 0n:
      Empty.absurd({1n+x == 0n : Nat}, ft(e))
    case 1n+x 1n+y:
      Equal.cong(Nat, Nat, k => 1n+k, x, y, eqn_true(x, y, e))

def or(a: Bool, b: Bool) -> Bool:
  match a:
    case True{}:
      True{}
    case False{}:
      b

def mem(+x: Nat, l: +List<Nat>) -> Bool:
  match l:
    case Nil{}:
      False{}
    case Con{h, t}:
      or(eqn(x, h), mem(x, t))

# case analysis on a computed Bool: the helper gets the Bool and its equation
law mem_head:
  for +x: Nat
  for +t: +List<Nat>
  {mem(x, Con{x, t}) == True{} : Bool}

def mem_head(x, t):
  %Equal.sym(Bool, eqn(x, x), True{}, eqn_refl(x)) : {or(_, mem(x, t)) == True{} : Bool}
  {==}

def mem_cons_go(+x: Nat, +h: Nat, +t: +List<Nat>, b: Bool, eb: {b == eqn(x, h) : Bool}, m: {or(b, mem(x, t)) == True{} : Bool}) -> Either<&1, &1, {x == h : Nat}, {mem(x, t) == True{} : Bool}>:
  match b:
    case True{}:
      Inl{eqn_true(x, h, Equal.sym(Bool, True{}, eqn(x, h), eb))}
    case False{}:
      Inr{m}

# x in h :: t means x == h or x in t
law mem_cons:
  for +x: Nat
  for +h: Nat
  for +t: +List<Nat>
  for m: {mem(x, Con{h, t}) == True{} : Bool}
  Either<&1, &1, {x == h : Nat}, {mem(x, t) == True{} : Bool}>

def mem_cons(x, h, t, m):
  mem_cons_go(x, h, t, eqn(x, h), {==}, m)

law mem_cons_ne:
  for +x: Nat
  for +h: Nat
  for +t: +List<Nat>
  for ne: {eqn(x, h) == False{} : Bool}
  {mem(x, Con{h, t}) == mem(x, t) : Bool}

def mem_cons_ne(x, h, t, ne):
  %Equal.sym(Bool, eqn(x, h), False{}, ne) : {or(_, mem(x, t)) == mem(x, t) : Bool}
  {==}

def app(a: +List<Nat>, b: +List<Nat>) -> +List<Nat>:
  match a:
    case Nil{}:
      b
    case Con{y, r}:
      Con{y, app(r, b)}

# hd of an empty list is 0; walks are never empty.
def hd(l: +List<Nat>) -> Nat:
  match l:
    case Nil{}:
      0n
    case Con{y, r}:
      y

def before.put(+y: Nat, rec: +List<Nat>, hit: Bool) -> +List<Nat>:
  match hit:
    case True{}:
      Nil{}
    case False{}:
      Con{y, rec}

# the elements of l in front of the first x (all of l if x is not in it)
def before(+x: Nat, l: +List<Nat>) -> +List<Nat>:
  match l:
    case Nil{}:
      Nil{}
    case Con{+y, r}:
      before.put(y, before(x, r), eqn(x, y))

def after.put(r: +List<Nat>, rec: +List<Nat>, hit: Bool) -> +List<Nat>:
  match hit:
    case True{}:
      r
    case False{}:
      rec

# the elements of l behind the first x
def after(+x: Nat, l: +List<Nat>) -> +List<Nat>:
  match l:
    case Nil{}:
      Nil{}
    case Con{y, +r}:
      after.put(r, after(x, r), eqn(x, y))

# eqn(x, y) is False when x != y as Nats
law eqn_false:
  for +a: Nat
  for +b: Nat
  for ne: {a == b : Nat} -> Empty
  {eqn(a, b) == False{} : Bool}

def eqn_false.go(+a: Nat, +b: Nat, c: Bool, ec: {c == eqn(a, b) : Bool}, ne: {a == b : Nat} -> Empty) -> {eqn(a, b) == False{} : Bool}:
  match c:
    case True{}:
      Empty.absurd({eqn(a, b) == False{} : Bool}, ne(eqn_true(a, b, Equal.sym(Bool, True{}, eqn(a, b), ec))))
    case False{}:
      Equal.sym(Bool, False{}, eqn(a, b), ec)

def eqn_false(a, b, ne):
  eqn_false.go(a, b, eqn(a, b), {==}, ne)

law tf:
  for e: {True{} == False{} : Bool}
  Empty

def tf(e):
  ft(Equal.sym(Bool, True{}, False{}, e))

# Not in l: before is all of l.
law before_nomem:
  for +x: Nat
  for +l: +List<Nat>
  for nm: {mem(x, l) == False{} : Bool}
  {before(x, l) == l : +List<Nat>}

def before_nomem.go(+x: Nat, +y: Nat, +r: +List<Nat>, c: Bool, ec: {c == eqn(x, y) : Bool}, nm: {or(c, mem(x, r)) == False{} : Bool}, ih: {mem(x, r) == False{} : Bool} -> {before(x, r) == r : +List<Nat>}) -> {before.put(y, before(x, r), c) == Con{y, r} : +List<Nat>}:
  match c:
    case True{}:
      Empty.absurd({Nil{} == Con{y, r} : +List<Nat>}, tf(nm))
    case False{}:
      %ih(nm) : {Con{y, before(x, r)} == Con{y, _} : +List<Nat>}
      {==}

def before_nomem(x, l, nm):
  match l:
    case Nil{}:
      {==}
    case Con{y, r}:
      before_nomem.go(x, y, r, eqn(x, y), {==}, nm, h => before_nomem(x, r, h))

# x in l: l is (before x) ++ [x] ++ (after x).
law split:
  for +x: Nat
  for +l: +List<Nat>
  for m: {mem(x, l) == True{} : Bool}
  {l == app(before(x, l), Con{x, after(x, l)}) : +List<Nat>}

def split.go(+x: Nat, +y: Nat, +r: +List<Nat>, c: Bool, ec: {c == eqn(x, y) : Bool}, m: {or(c, mem(x, r)) == True{} : Bool}, ih: {mem(x, r) == True{} : Bool} -> {r == app(before(x, r), Con{x, after(x, r)}) : +List<Nat>}) -> {Con{y, r} == app(before.put(y, before(x, r), c), Con{x, after.put(r, after(x, r), c)}) : +List<Nat>}:
  match c:
    case True{}:
      %eqn_true(x, y, Equal.sym(Bool, True{}, eqn(x, y), ec)) : {Con{_, r} == Con{x, r} : +List<Nat>}
      {==}
    case False{}:
      Equal.cong(+List<Nat>, +List<Nat>, k => Con{y, k}, r, app(before(x, r), Con{x, after(x, r)}), ih(m))

def split(x, l, m):
  match l:
    case Nil{}:
      Empty.absurd({Nil{} == app(Nil{}, Con{x, Nil{}}) : +List<Nat>}, ft(m))
    case Con{y, r}:
      split.go(x, y, r, eqn(x, y), {==}, m, h => split(x, r, h))

# x is not in front of its first occurrence.
law before_no:
  for +x: Nat
  for +l: +List<Nat>
  {mem(x, before(x, l)) == False{} : Bool}

def before_no.go(+x: Nat, +y: Nat, +r: +List<Nat>, c: Bool, ec: {c == eqn(x, y) : Bool}, ih: {mem(x, before(x, r)) == False{} : Bool}) -> {mem(x, before.put(y, before(x, r), c)) == False{} : Bool}:
  match c:
    case True{}:
      {==}
    case False{}:
      %ec : {or(_, mem(x, before(x, r))) == False{} : Bool}
      ih

def before_no(x, l):
  match l:
    case Nil{}:
      {==}
    case Con{y, r}:
      before_no.go(x, y, r, eqn(x, y), {==}, before_no(x, r))

law or_ff:
  for a: Bool
  for b: Bool
  for ea: {a == False{} : Bool}
  for eb: {b == False{} : Bool}
  {or(a, b) == False{} : Bool}

def or_ff(a, b, ea, eb):
  match a:
    case True{}:
      Empty.absurd({True{} == False{} : Bool}, tf(ea))
    case False{}:
      eb

law mem_app_false:
  for +x: Nat
  for +a: +List<Nat>
  for +b: +List<Nat>
  for na: {mem(x, a) == False{} : Bool}
  for nb: {mem(x, b) == False{} : Bool}
  {mem(x, app(a, b)) == False{} : Bool}

def mem_app_false.go(+x: Nat, +y: Nat, +r: +List<Nat>, +b: +List<Nat>, c: Bool, ec: {c == eqn(x, y) : Bool}, na: {or(c, mem(x, r)) == False{} : Bool}, ih: {mem(x, r) == False{} : Bool} -> {mem(x, app(r, b)) == False{} : Bool}) -> {or(c, mem(x, app(r, b))) == False{} : Bool}:
  match c:
    case True{}:
      na
    case False{}:
      ih(na)

def mem_app_false(x, a, b, na, nb):
  match a:
    case Nil{}:
      nb
    case Con{y, r}:
      mem_app_false.go(x, y, r, b, eqn(x, y), {==}, na, h => mem_app_false(x, r, b, h, nb))

# or(c, x) True with c False: x True
law or_fl:
  for c: Bool
  for -x: Bool
  for ec: {False{} == c : Bool}
  for m: {or(c, x) == True{} : Bool}
  {x == True{} : Bool}

def or_fl(c, x, ec, m):
  match c:
    case True{}:
      Empty.absurd({x == True{} : Bool}, ft(ec))
    case False{}:
      m

# d occurs in front of the first h: then h does not occur in front of the first d.
law bd_no_h:
  for +d: Nat
  for +h: Nat
  for +w: +List<Nat>
  for m: {mem(d, before(h, w)) == True{} : Bool}
  {mem(h, before(d, w)) == False{} : Bool}

def bd_no_h.go(+d: Nat, +h: Nat, +y: Nat, +r: +List<Nat>, ch: Bool, ech: {ch == eqn(h, y) : Bool}, cd: Bool, ecd: {cd == eqn(d, y) : Bool}, m: {mem(d, before.put(y, before(h, r), ch)) == True{} : Bool}, ih: {mem(d, before(h, r)) == True{} : Bool} -> {mem(h, before(d, r)) == False{} : Bool}) -> {mem(h, before.put(y, before(d, r), cd)) == False{} : Bool}:
  match ch cd:
    case True{} _:
      Empty.absurd({mem(h, before.put(y, before(d, r), cd)) == False{} : Bool}, ft(m))
    case False{} True{}:
      {==}
    case False{} False{}:
      %ech : {or(_, mem(h, before(d, r))) == False{} : Bool}
      ih(or_fl(eqn(d, y), mem(d, before(h, r)), ecd, m))

def bd_no_h(d, h, w, m):
  match w:
    case Nil{}:
      {==}
    case Con{y, r}:
      bd_no_h.go(d, h, y, r, eqn(h, y), {==}, eqn(d, y), {==}, m, k => bd_no_h(d, h, r, k))

law hd_app:
  for +v: +List<Nat>
  for +x: Nat
  for +a: +List<Nat>
  for +b: +List<Nat>
  {hd(app(v, Con{x, a})) == hd(app(v, Con{x, b})) : Nat}

def hd_app(v, x, a, b):
  match v:
    case Nil{}:
      {==}
    case Con{y, r}:
      {==}

# or(a, b) True and b implies b': or(a, b') True
law or_rt:
  for a: Bool
  for b: Bool
  for -b2: Bool
  for m: {or(a, b) == True{} : Bool}
  for f: {b == True{} : Bool} -> {b2 == True{} : Bool}
  {or(a, b2) == True{} : Bool}

def or_rt(a, b, b2, m, f):
  match a:
    case True{}:
      {==}
    case False{}:
      f(m)

# something in front of the first x is in the list
law mem_before:
  for +z: Nat
  for +x: Nat
  for +l: +List<Nat>
  for m: {mem(z, before(x, l)) == True{} : Bool}
  {mem(z, l) == True{} : Bool}

def mem_before.go(+z: Nat, +x: Nat, +y: Nat, +r: +List<Nat>, c: Bool, m: {mem(z, before.put(y, before(x, r), c)) == True{} : Bool}, ih: {mem(z, before(x, r)) == True{} : Bool} -> {mem(z, r) == True{} : Bool}) -> {or(eqn(z, y), mem(z, r)) == True{} : Bool}:
  match c:
    case True{}:
      Empty.absurd({or(eqn(z, y), mem(z, r)) == True{} : Bool}, ft(m))
    case False{}:
      or_rt(eqn(z, y), mem(z, before(x, r)), mem(z, r), m, ih)

def mem_before(z, x, l, m):
  match l:
    case Nil{}:
      m
    case Con{y, r}:
      mem_before.go(z, x, y, r, eqn(x, y), m, k => mem_before(z, x, r, k))

law bclash:
  for -b: Bool
  for e1: {b == True{} : Bool}
  for e2: {b == False{} : Bool}
  Empty

def bclash(b, e1, e2):
  tf(Equal.trans(Bool, True{}, b, False{}, Equal.sym(Bool, b, True{}, e1), e2))

law mem_hit:
  for +h: Nat
  for +z: Nat
  for +r: +List<Nat>
  for zh: {z == h : Nat}
  {mem(h, Con{z, r}) == True{} : Bool}

def mem_hit(h, z, r, zh):
  %zh : {mem(_, Con{z, r}) == True{} : Bool}
  mem_head(z, r)

law or_false_r:
  for a: Bool
  for -b: Bool
  for e: {or(a, b) == False{} : Bool}
  {b == False{} : Bool}

def or_false_r(a, b, e):
  match a:
    case True{}:
      Empty.absurd({b == False{} : Bool}, tf(e))
    case False{}:
      e

law or_tf:
  for a: Bool
  for -b: Bool
  for m: {or(a, b) == True{} : Bool}
  for nb: {b == False{} : Bool}
  {a == True{} : Bool}

def or_tf(a, b, m, nb):
  match a:
    case True{}:
      {==}
    case False{}:
      Empty.absurd({False{} == True{} : Bool}, bclash(b, m, nb))

law mem_tail:
  for +x: Nat
  for +y: Nat
  for +l: +List<Nat>
  for m: {mem(x, l) == True{} : Bool}
  {mem(x, Con{y, l}) == True{} : Bool}

def mem_tail.go(c: Bool, -b: Bool, m: {b == True{} : Bool}) -> {or(c, b) == True{} : Bool}:
  match c:
    case True{}:
      {==}
    case False{}:
      m

def mem_tail(x, y, l, m):
  mem_tail.go(eqn(x, y), mem(x, l), m)

law mem_app_r:
  for +x: Nat
  for +a: +List<Nat>
  for +b: +List<Nat>
  for m: {mem(x, b) == True{} : Bool}
  {mem(x, app(a, b)) == True{} : Bool}

def mem_app_r(x, a, b, m):
  match a:
    case Nil{}:
      m
    case Con{y, r}:
      mem_tail(x, y, app(r, b), mem_app_r(x, r, b, m))
