# cfg.bend: walks of a control flow graph, dominance, and the two path facts that rule B3
# of go-bce relies on.
#
# A CFG is an edge relation E(a, b) (an edge from block a to block b) and an entry block.
# A walk is written latest block first: Con{x, r} is a walk that is now at x, and r is the
# walk that led there. It ends (at its oldest element) at entry. t dominates x when every
# walk now at x contains t.
import Base
import ./list.bend as L

# VW(E, entry, x, r): x :: r is a walk.
def VW(-E: Nat -> Nat -> Data, +entry: Nat, +x: Nat, r: +List<Nat>) -> Data:
  match r:
    case Nil{}:
      {x == entry : Nat}
    case Con{y, r2}:
      Sigma<&2, &2, E(y, x), _ => VW(E, entry, y, r2)>

def Valid(-E: Nat -> Nat -> Data, +entry: Nat, w: +List<Nat>) -> Data:
  match w:
    case Nil{}:
      Empty
    case Con{x, r}:
      VW(E, entry, x, r)

# t dominates x
def Dom(-E: Nat -> Nat -> Data, +entry: Nat, +t: Nat, +x: Nat) -> Type:
  @w: +List<Nat> -> Valid(E, entry, w) -> {L.hd(w) == x : Nat} -> {L.mem(t, w) == True{} : Bool}

# h is the only predecessor of t
def SP(-E: Nat -> Nat -> Data, +h: Nat, +t: Nat) -> Type:
  @y: Nat -> E(y, t) -> {y == h : Nat}

# V1: a suffix of a walk that starts at h is a walk.
law vw_suffix:
  for -E: Nat -> Nat -> Data
  for +entry: Nat
  for +v: +List<Nat>
  for +y: Nat
  for +h: Nat
  for +r: +List<Nat>
  for p: VW(E, entry, y, L.app(v, Con{h, r}))
  VW(E, entry, h, r)

def vw_suffix(E, entry, v, y, h, r, p):
  match v:
    case Nil{}:
      (e, q) = p
      q
    case Con{z, v2}:
      (e, q) = p
      vw_suffix(E, entry, v2, z, h, r, q)

# V2: replace the part of a walk behind an h by another walk from h.
law vw_splice:
  for -E: Nat -> Nat -> Data
  for +entry: Nat
  for +v: +List<Nat>
  for +y: Nat
  for +h: Nat
  for +r: +List<Nat>
  for +r2: +List<Nat>
  for p: VW(E, entry, y, L.app(v, Con{h, r}))
  for q: VW(E, entry, h, r2)
  VW(E, entry, y, L.app(v, Con{h, r2}))

def vw_splice(E, entry, v, y, h, r, r2, p, q):
  match v:
    case Nil{}:
      (e, k) = p
      (e, q)
    case Con{z, v2}:
      (e, k) = p
      (e, vw_splice(E, entry, v2, z, h, r, r2, k, q))

law valid_suffix:
  for -E: Nat -> Nat -> Data
  for +entry: Nat
  for +v: +List<Nat>
  for +h: Nat
  for +r: +List<Nat>
  for p: Valid(E, entry, L.app(v, Con{h, r}))
  VW(E, entry, h, r)

def valid_suffix(E, entry, v, h, r, p):
  match v:
    case Nil{}:
      p
    case Con{z, v2}:
      vw_suffix(E, entry, v2, z, h, r, p)

law valid_splice:
  for -E: Nat -> Nat -> Data
  for +entry: Nat
  for +v: +List<Nat>
  for +h: Nat
  for +r: +List<Nat>
  for +r2: +List<Nat>
  for p: Valid(E, entry, L.app(v, Con{h, r}))
  for q: VW(E, entry, h, r2)
  Valid(E, entry, L.app(v, Con{h, r2}))

def valid_splice(E, entry, v, h, r, r2, p, q):
  match v:
    case Nil{}:
      q
    case Con{z, v2}:
      vw_splice(E, entry, v2, z, h, r, r2, p, q)

# V5: a walk at t whose past has no h does not exist (t has the single predecessor h, and
# t is not the entry block).
law not_at_t:
  for -E: Nat -> Nat -> Data
  for +entry: Nat
  for +r: +List<Nat>
  for +y: Nat
  for +h: Nat
  for +t: Nat
  for sp: SP(E, h, t)
  for +te: {L.eqn(t, entry) == False{} : Bool}
  for p: VW(E, entry, y, r)
  for nh: {L.mem(h, r) == False{} : Bool}
  for yt: {y == t : Nat}
  Empty

def not_at_t(E, entry, r, y, h, t, sp, te, p, nh, yt):
  match r:
    case Nil{}:
      L.tf(Equal.trans(Bool, True{}, L.eqn(t, entry), False{}, (%yt : {True{} == L.eqn(_, entry) : Bool}; Equal.sym(Bool, L.eqn(y, entry), True{}, (%p : {L.eqn(y, _) == True{} : Bool}; L.eqn_refl(y)))), te))
    case Con{z, r2}:
      (e, k) = p
      zh : {z == h : Nat} = sp(z, (%yt : E(z, _); e))
      L.bclash(L.mem(h, Con{z, r2}), L.mem_hit(h, z, r2, zh), nh)

# V4: in a walk whose past has no h, t does not occur.
law no_t:
  for -E: Nat -> Nat -> Data
  for +entry: Nat
  for +l: +List<Nat>
  for +y: Nat
  for +h: Nat
  for +t: Nat
  for sp: SP(E, h, t)
  for +te: {L.eqn(t, entry) == False{} : Bool}
  for p: VW(E, entry, y, l)
  for nh: {L.mem(h, l) == False{} : Bool}
  {L.mem(t, l) == False{} : Bool}

def no_t.go(-E: Nat -> Nat -> Data, +entry: Nat, +z: Nat, +l2: +List<Nat>, +h: Nat, +t: Nat, c: Bool, ec: {c == L.eqn(t, z) : Bool}, sp: SP(E, h, t), +te: {L.eqn(t, entry) == False{} : Bool}, k: VW(E, entry, z, l2), +nh2: {L.mem(h, l2) == False{} : Bool}, rec: SP(E, h, t) -> {L.eqn(t, entry) == False{} : Bool} -> VW(E, entry, z, l2) -> {L.mem(t, l2) == False{} : Bool}) -> {L.or(c, L.mem(t, l2)) == False{} : Bool}:
  match c:
    case True{}:
      Empty.absurd({True{} == False{} : Bool}, not_at_t(E, entry, l2, z, h, t, sp, te, k, nh2, Equal.sym(Nat, t, z, L.eqn_true(t, z, Equal.sym(Bool, True{}, L.eqn(t, z), ec)))))
    case False{}:
      rec(sp, te, k)

def no_t(E, entry, l, y, h, t, sp, te, p, nh):
  match l:
    case Nil{}:
      {==}
    case Con{z, l2}:
      (e, k) = p
      +nh2 : {L.mem(h, l2) == False{} : Bool} = L.or_false_r(L.eqn(h, z), L.mem(h, l2), nh)
      no_t.go(E, entry, z, l2, h, t, L.eqn(t, z), {==}, sp, te, k, nh2, sp2 => te2 => k2 => no_t(E, entry, l2, z, h, t, sp2, te2, k2, nh2))

def Deep(-E: Nat -> Nat -> Data, +entry: Nat, +h: Nat) -> Type:
  &r2: +List<Nat> -> (VW(E, entry, h, r2) & {L.mem(h, r2) == False{} : Bool})

# V3: a walk that contains h has a walk from h (to entry) with no other h: cut at the
# oldest h.
law deepest:
  for -E: Nat -> Nat -> Data
  for +entry: Nat
  for +r: +List<Nat>
  for +x: Nat
  for +h: Nat
  for p: VW(E, entry, x, r)
  for m: {L.mem(h, Con{x, r}) == True{} : Bool}
  Deep(E, entry, h)

def deepest.go(-E: Nat -> Nat -> Data, +entry: Nat, +x: Nat, +y: Nat, +r2: +List<Nat>, +h: Nat, c: Bool, ec: {c == L.mem(h, Con{y, r2}) : Bool}, p: VW(E, entry, x, Con{y, r2}), m: {L.or(L.eqn(h, x), c) == True{} : Bool}, ih: {L.mem(h, Con{y, r2}) == True{} : Bool} -> VW(E, entry, y, r2) -> Deep(E, entry, h)) -> Deep(E, entry, h):
  match c:
    case True{}:
      (e, k) = p
      ih(Equal.sym(Bool, True{}, L.mem(h, Con{y, r2}), ec), k)
    case False{}:
      hx : {h == x : Nat} = L.eqn_true(h, x, L.or_tf(L.eqn(h, x), False{}, m, {==}))
      (Con{y, r2}, ((%Equal.sym(Nat, h, x, hx) : VW(E, entry, _, Con{y, r2}); p), Equal.sym(Bool, False{}, L.mem(h, Con{y, r2}), ec)))

def deepest(E, entry, r, x, h, p, m):
  match r:
    case Nil{}:
      hx : {h == x : Nat} = L.eqn_true(h, x, L.or_tf(L.eqn(h, x), False{}, m, {==}))
      (Nil{}, ((%Equal.sym(Nat, h, x, hx) : {_ == entry : Nat}; p), {==}))
    case Con{y, r2}:
      deepest.go(E, entry, x, y, r2, h, L.mem(h, Con{y, r2}), {==}, p, m, mm => k => deepest(E, entry, r2, y, h, k, mm))

def Clean(-E: Nat -> Nat -> Data, +entry: Nat, +h: Nat, +t: Nat) -> Type:
  &r2: +List<Nat> -> Sigma<&2, &2, VW(E, entry, h, r2), _ => {L.mem(t, Con{h, r2}) == False{} : Bool}>

def clean.go(-E: Nat -> Nat -> Data, +entry: Nat, +h: Nat, +t: Nat, sp1: SP(E, h, t), sp2: SP(E, h, t), +te: {L.eqn(t, entry) == False{} : Bool}, d: Deep(E, entry, h)) -> Clean(E, entry, h, t):
  (+r2, q) = d
  (+vw2, +nh2) = q
  nt : {L.mem(t, r2) == False{} : Bool} = no_t(E, entry, r2, h, h, t, sp1, te, vw2, nh2)
  ht : {L.eqn(t, h) == False{} : Bool} = L.eqn_false(t, h, th => not_at_t(E, entry, r2, h, h, t, sp2, te, vw2, nh2, Equal.sym(Nat, t, h, th)))
  (r2, (vw2, L.or_ff(L.eqn(t, h), L.mem(t, r2), ht, nt)))

# From a walk at h: a walk at h whose past has no h and which has no t at all.
law clean:
  for -E: Nat -> Nat -> Data
  for +entry: Nat
  for +a: +List<Nat>
  for +h: Nat
  for +t: Nat
  for sp1: SP(E, h, t)
  for sp2: SP(E, h, t)
  for +te: {L.eqn(t, entry) == False{} : Bool}
  for +vw: VW(E, entry, h, a)
  Clean(E, entry, h, t)

def clean(E, entry, a, h, t, sp1, sp2, te, vw):
  clean.go(E, entry, h, t, sp1, sp2, te, deepest(E, entry, a, h, h, vw, L.mem_head(h, a)))

def t_after_h.cut(-E: Nat -> Nat -> Data, +entry: Nat, +w: +List<Nat>, +x: Nat, +h: Nat, +t: Nat, dom: Dom(E, entry, t, x), +pw: Valid(E, entry, L.app(L.before(h, w), Con{h, L.after(h, w)})), +hw: {L.hd(L.app(L.before(h, w), Con{h, L.after(h, w)})) == x : Nat}, +nv: {L.mem(t, L.before(h, w)) == False{} : Bool}, c: Clean(E, entry, h, t)) -> Empty:
  (+r2, q) = c
  (+vw2, +nt) = q
  pw2 : Valid(E, entry, L.app(L.before(h, w), Con{h, r2})) = valid_splice(E, entry, L.before(h, w), h, L.after(h, w), r2, pw, vw2)
  hw2 : {L.hd(L.app(L.before(h, w), Con{h, r2})) == x : Nat} = Equal.trans(Nat, L.hd(L.app(L.before(h, w), Con{h, r2})), L.hd(L.app(L.before(h, w), Con{h, L.after(h, w)})), x, Equal.sym(Nat, L.hd(L.app(L.before(h, w), Con{h, L.after(h, w)})), L.hd(L.app(L.before(h, w), Con{h, r2})), L.hd_app(L.before(h, w), h, L.after(h, w), r2)), hw)
  L.bclash(L.mem(t, L.app(L.before(h, w), Con{h, r2})), dom(L.app(L.before(h, w), Con{h, r2}), pw2, hw2), L.mem_app_false(t, L.before(h, w), Con{h, r2}, nv, nt))

def t_after_h.go(-E: Nat -> Nat -> Data, +entry: Nat, +w: +List<Nat>, +x: Nat, +h: Nat, +t: Nat, c: Bool, ec: {c == L.mem(t, L.before(h, w)) : Bool}, mh: Bool, emh: {mh == L.mem(h, w) : Bool}, sp1: SP(E, h, t), sp2: SP(E, h, t), +te: {L.eqn(t, entry) == False{} : Bool}, dom: Dom(E, entry, t, x), +pw: Valid(E, entry, w), +hw: {L.hd(w) == x : Nat}) -> {c == True{} : Bool}:
  match c mh:
    case True{} _:
      {==}
    case False{} False{}:
      bw : {L.before(h, w) == w : +List<Nat>} = L.before_nomem(h, w, Equal.sym(Bool, False{}, L.mem(h, w), emh))
      nw : {L.mem(t, w) == False{} : Bool} = (%bw : {L.mem(t, _) == False{} : Bool}; Equal.sym(Bool, False{}, L.mem(t, L.before(h, w)), ec))
      Empty.absurd({False{} == True{} : Bool}, L.bclash(L.mem(t, w), dom(w, pw, hw), nw))
    case False{} True{}:
      +ws : {w == L.app(L.before(h, w), Con{h, L.after(h, w)}) : +List<Nat>} = L.split(h, w, Equal.sym(Bool, True{}, L.mem(h, w), emh))
      +pw1 : Valid(E, entry, L.app(L.before(h, w), Con{h, L.after(h, w)})) = (%ws : Valid(E, entry, _); pw)
      +hw1 : {L.hd(L.app(L.before(h, w), Con{h, L.after(h, w)})) == x : Nat} = (%ws : {L.hd(_) == x : Nat}; hw)
      vwa : VW(E, entry, h, L.after(h, w)) = valid_suffix(E, entry, L.before(h, w), h, L.after(h, w), pw1)
      Empty.absurd({False{} == True{} : Bool}, t_after_h.cut(E, entry, w, x, h, t, dom, pw1, hw1, Equal.sym(Bool, False{}, L.mem(t, L.before(h, w)), ec), clean(E, entry, L.after(h, w), h, t, sp1, sp2, te, vwa)))

# T1: t has the single predecessor h, t is not the entry block, and t dominates x. Then on
# every walk now at x, t occurs after the last h (in front of the first h, latest first).
law t_after_h:
  for -E: Nat -> Nat -> Data
  for +entry: Nat
  for +w: +List<Nat>
  for +x: Nat
  for +h: Nat
  for +t: Nat
  for sp1: SP(E, h, t)
  for sp2: SP(E, h, t)
  for +te: {L.eqn(t, entry) == False{} : Bool}
  for dom: Dom(E, entry, t, x)
  for +pw: Valid(E, entry, w)
  for +hw: {L.hd(w) == x : Nat}
  {L.mem(t, L.before(h, w)) == True{} : Bool}

def t_after_h(E, entry, w, x, h, t, sp1, sp2, te, dom, pw, hw):
  t_after_h.go(E, entry, w, x, h, t, L.mem(t, L.before(h, w)), {==}, L.mem(h, w), {==}, sp1, sp2, te, dom, pw, hw)

# d == h contradicts eqn(d, h) == False
law ne_of:
  for +d: Nat
  for +h: Nat
  for +dh: {L.eqn(d, h) == False{} : Bool}
  for e: {d == h : Nat}
  Empty

def ne_of(d, h, dh, e):
  L.bclash(L.eqn(d, h), (%e : {L.eqn(d, _) == True{} : Bool}; L.eqn_refl(d)), dh)

def no_t_front.nil(-E: Nat -> Nat -> Data, +y: Nat, +d: Nat, +h: Nat, +t: Nat, c: Bool, ec: {c == L.eqn(t, y) : Bool}, sp: SP(E, h, t), +e: E(d, y), +dh: {L.eqn(d, h) == False{} : Bool}) -> {L.or(c, False{}) == False{} : Bool}:
  match c:
    case True{}:
      +ty : {t == y : Nat} = L.eqn_true(t, y, Equal.sym(Bool, True{}, L.eqn(t, y), ec))
      Empty.absurd({True{} == False{} : Bool}, ne_of(d, h, dh, sp(d, (%Equal.sym(Nat, t, y, ty) : E(d, _); e))))
    case False{}:
      {==}

def no_t_front.con(-E: Nat -> Nat -> Data, +y: Nat, +z: Nat, +v2: +List<Nat>, +h: Nat, +t: Nat, c: Bool, ec: {c == L.eqn(t, y) : Bool}, sp: SP(E, h, t), +e: E(z, y), +nh2: {L.mem(h, Con{z, v2}) == False{} : Bool}, rec: SP(E, h, t) -> {L.mem(t, Con{z, v2}) == False{} : Bool}) -> {L.or(c, L.mem(t, Con{z, v2})) == False{} : Bool}:
  match c:
    case True{}:
      +ty : {t == y : Nat} = L.eqn_true(t, y, Equal.sym(Bool, True{}, L.eqn(t, y), ec))
      zh : {z == h : Nat} = sp(z, (%Equal.sym(Nat, t, y, ty) : E(z, _); e))
      Empty.absurd({True{} == False{} : Bool}, L.bclash(L.mem(h, Con{z, v2}), L.mem_hit(h, z, v2, zh), nh2))
    case False{}:
      rec(sp)

# V6: in a walk y :: v ++ [d] ++ s where neither y :: v has h nor d is h, t is not in y :: v
# (t's predecessor would be an h).
law no_t_front:
  for -E: Nat -> Nat -> Data
  for +entry: Nat
  for +v: +List<Nat>
  for +y: Nat
  for +d: Nat
  for +s: +List<Nat>
  for +h: Nat
  for +t: Nat
  for sp: SP(E, h, t)
  for +p: VW(E, entry, y, L.app(v, Con{d, s}))
  for +nh: {L.mem(h, Con{y, v}) == False{} : Bool}
  for +dh: {L.eqn(d, h) == False{} : Bool}
  {L.mem(t, Con{y, v}) == False{} : Bool}

def no_t_front(E, entry, v, y, d, s, h, t, sp, p, nh, dh):
  match v:
    case Nil{}:
      (+e, +k) = p
      no_t_front.nil(E, y, d, h, t, L.eqn(t, y), {==}, sp, e, dh)
    case Con{z, v2}:
      (+e, +k) = p
      +nh2 : {L.mem(h, Con{z, v2}) == False{} : Bool} = L.or_false_r(L.eqn(h, y), L.mem(h, Con{z, v2}), nh)
      no_t_front.con(E, y, z, v2, h, t, L.eqn(t, y), {==}, sp, e, nh2, sp2 => no_t_front(E, entry, v2, z, d, s, h, t, sp2, k, nh2, dh))

law no_t_front_valid:
  for -E: Nat -> Nat -> Data
  for +entry: Nat
  for +v: +List<Nat>
  for +d: Nat
  for +s: +List<Nat>
  for +h: Nat
  for +t: Nat
  for sp: SP(E, h, t)
  for +p: Valid(E, entry, L.app(v, Con{d, s}))
  for +nh: {L.mem(h, v) == False{} : Bool}
  for +dh: {L.eqn(d, h) == False{} : Bool}
  {L.mem(t, v) == False{} : Bool}

def no_t_front_valid(E, entry, v, d, s, h, t, sp, p, nh, dh):
  match v:
    case Nil{}:
      {==}
    case Con{y, v2}:
      no_t_front(E, entry, v2, y, d, s, h, t, sp, p, nh, dh)

# d dominates h, d != h, and the walk at d has no other d: then it has no h.
def d_noh.go(-E: Nat -> Nat -> Data, +entry: Nat, +r2: +List<Nat>, +d: Nat, +h: Nat, c: Bool, ec: {c == L.mem(h, r2) : Bool}, +dh: {L.eqn(d, h) == False{} : Bool}, domdh: Dom(E, entry, d, h), +vw2: VW(E, entry, d, r2), +nd2: {L.mem(d, r2) == False{} : Bool}) -> {c == False{} : Bool}:
  match c:
    case False{}:
      {==}
    case True{}:
      +rs : {r2 == L.app(L.before(h, r2), Con{h, L.after(h, r2)}) : +List<Nat>} = L.split(h, r2, Equal.sym(Bool, True{}, L.mem(h, r2), ec))
      +vw3 : VW(E, entry, d, L.app(L.before(h, r2), Con{h, L.after(h, r2)})) = (%rs : VW(E, entry, d, _); vw2)
      +vh : VW(E, entry, h, L.after(h, r2)) = vw_suffix(E, entry, L.before(h, r2), d, h, L.after(h, r2), vw3)
      +dm : {L.mem(d, Con{h, L.after(h, r2)}) == True{} : Bool} = domdh(Con{h, L.after(h, r2)}, vh, {==})
      +dm2 : {L.mem(d, r2) == True{} : Bool} = (%Equal.sym(+List<Nat>, r2, L.app(L.before(h, r2), Con{h, L.after(h, r2)}), rs) : {L.mem(d, _) == True{} : Bool}; L.mem_app_r(d, L.before(h, r2), Con{h, L.after(h, r2)}, dm))
      Empty.absurd({True{} == False{} : Bool}, L.bclash(L.mem(d, r2), dm2, nd2))

def d_after_h.step(-E: Nat -> Nat -> Data, +entry: Nat, +w: +List<Nat>, +x: Nat, +h: Nat, +t: Nat, +d: Nat, +dh: {L.eqn(d, h) == False{} : Bool}, domdh: Dom(E, entry, d, h), sp1: SP(E, h, t), sp2: SP(E, h, t), sp3: SP(E, h, t), +te: {L.eqn(t, entry) == False{} : Bool}, domtx: Dom(E, entry, t, x), +pw1: Valid(E, entry, L.app(L.before(d, w), Con{d, L.after(d, w)})), +hw1: {L.hd(L.app(L.before(d, w), Con{d, L.after(d, w)})) == x : Nat}, +nh1: {L.mem(h, L.before(d, w)) == False{} : Bool}, dp: Deep(E, entry, d)) -> Empty:
  (+r2, q) = dp
  (+vw2, +nd2) = q
  +nh2 : {L.mem(h, r2) == False{} : Bool} = d_noh.go(E, entry, r2, d, h, L.mem(h, r2), {==}, dh, domdh, vw2, nd2)
  +nt2 : {L.mem(t, r2) == False{} : Bool} = no_t(E, entry, r2, d, h, t, sp1, te, vw2, nh2)
  +td : {L.eqn(t, d) == False{} : Bool} = L.eqn_false(t, d, e => not_at_t(E, entry, r2, d, h, t, sp2, te, vw2, nh2, Equal.sym(Nat, t, d, e)))
  +ntd : {L.mem(t, Con{d, r2}) == False{} : Bool} = L.or_ff(L.eqn(t, d), L.mem(t, r2), td, nt2)
  +nt1 : {L.mem(t, L.before(d, w)) == False{} : Bool} = no_t_front_valid(E, entry, L.before(d, w), d, L.after(d, w), h, t, sp3, pw1, nh1, dh)
  +pw3 : Valid(E, entry, L.app(L.before(d, w), Con{d, r2})) = valid_splice(E, entry, L.before(d, w), d, L.after(d, w), r2, pw1, vw2)
  +hw3 : {L.hd(L.app(L.before(d, w), Con{d, r2})) == x : Nat} = Equal.trans(Nat, L.hd(L.app(L.before(d, w), Con{d, r2})), L.hd(L.app(L.before(d, w), Con{d, L.after(d, w)})), x, Equal.sym(Nat, L.hd(L.app(L.before(d, w), Con{d, L.after(d, w)})), L.hd(L.app(L.before(d, w), Con{d, r2})), L.hd_app(L.before(d, w), d, L.after(d, w), r2)), hw1)
  L.bclash(L.mem(t, L.app(L.before(d, w), Con{d, r2})), domtx(L.app(L.before(d, w), Con{d, r2}), pw3, hw3), L.mem_app_false(t, L.before(d, w), Con{d, r2}, nt1, ntd))

def d_after_h.go(-E: Nat -> Nat -> Data, +entry: Nat, +w: +List<Nat>, +x: Nat, +h: Nat, +t: Nat, +d: Nat, c: Bool, ec: {c == L.mem(d, L.before(h, w)) : Bool}, +dh: {L.eqn(d, h) == False{} : Bool}, domdh: Dom(E, entry, d, h), sp1: SP(E, h, t), sp2: SP(E, h, t), sp3: SP(E, h, t), +te: {L.eqn(t, entry) == False{} : Bool}, domtx: Dom(E, entry, t, x), +pw: Valid(E, entry, w), +hw: {L.hd(w) == x : Nat}) -> {c == False{} : Bool}:
  match c:
    case False{}:
      {==}
    case True{}:
      +m1 : {L.mem(d, L.before(h, w)) == True{} : Bool} = Equal.sym(Bool, True{}, L.mem(d, L.before(h, w)), ec)
      +nh1 : {L.mem(h, L.before(d, w)) == False{} : Bool} = L.bd_no_h(d, h, w, m1)
      +ws : {w == L.app(L.before(d, w), Con{d, L.after(d, w)}) : +List<Nat>} = L.split(d, w, L.mem_before(d, h, w, m1))
      +pw1 : Valid(E, entry, L.app(L.before(d, w), Con{d, L.after(d, w)})) = (%ws : Valid(E, entry, _); pw)
      +hw1 : {L.hd(L.app(L.before(d, w), Con{d, L.after(d, w)})) == x : Nat} = (%ws : {L.hd(_) == x : Nat}; hw)
      +vws : VW(E, entry, d, L.after(d, w)) = valid_suffix(E, entry, L.before(d, w), d, L.after(d, w), pw1)
      Empty.absurd({True{} == False{} : Bool}, d_after_h.step(E, entry, w, x, h, t, d, dh, domdh, sp1, sp2, sp3, te, domtx, pw1, hw1, nh1, deepest(E, entry, L.after(d, w), d, d, vws, L.mem_head(d, L.after(d, w)))))

# T2: d dominates h and d != h; t has the single predecessor h, is not the entry block, and
# dominates x. Then on every walk now at x, d does not occur after the last h. (So a value
# defined in d still has the value it had at the last visit of h.)
law d_after_h:
  for -E: Nat -> Nat -> Data
  for +entry: Nat
  for +w: +List<Nat>
  for +x: Nat
  for +h: Nat
  for +t: Nat
  for +d: Nat
  for +dh: {L.eqn(d, h) == False{} : Bool}
  for domdh: Dom(E, entry, d, h)
  for sp1: SP(E, h, t)
  for sp2: SP(E, h, t)
  for sp3: SP(E, h, t)
  for +te: {L.eqn(t, entry) == False{} : Bool}
  for domtx: Dom(E, entry, t, x)
  for +pw: Valid(E, entry, w)
  for +hw: {L.hd(w) == x : Nat}
  {L.mem(d, L.before(h, w)) == False{} : Bool}

def d_after_h(E, entry, w, x, h, t, d, dh, domdh, sp1, sp2, sp3, te, domtx, pw, hw):
  d_after_h.go(E, entry, w, x, h, t, d, L.mem(d, L.before(h, w)), {==}, dh, domdh, sp1, sp2, sp3, te, domtx, pw, hw)
