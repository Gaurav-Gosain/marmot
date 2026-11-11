# word.bend: machine words as Nats. A w-bit word is a Nat below mod = 2^w; half = 2^(w-1).
# Signed values: a word below half is non-negative, a word at or above half is negative.
# The proofs keep half and mod abstract (only half <= mod is assumed where needed), so they
# hold for 64-bit words (half = 2^63) and for every other width.
import Base
import ./nat.bend as N

def slt.go(p: Bool, q: Bool, r: Bool) -> Bool:
  match p q:
    case True{} True{}:
      r
    case True{} False{}:
      False{}
    case False{} True{}:
      True{}
    case False{} False{}:
      r

# signed a < b
def slt(+half: Nat, +a: Nat, +b: Nat) -> Bool:
  slt.go(N.ltb(a, half), N.ltb(b, half), N.ltb(a, b))

# the compare of rule B3: "slt" (sgn True) or "ult" (sgn False)
def cmp(sgn: Bool, +half: Nat, +a: Nat, +b: Nat) -> Bool:
  match sgn:
    case True{}:
      slt(half, a, b)
    case False{}:
      N.ltb(a, b)

def wrap1.go(c: Bool, +a: Nat) -> Nat:
  match c:
    case True{}:
      1n+a
    case False{}:
      0n

# a + 1 modulo mod (for a < mod)
def wrap1(+mod: Nat, +a: Nat) -> Nat:
  wrap1.go(N.ltb(1n+a, mod), a)

law wrap1_small:
  for +mod: Nat
  for +a: Nat
  for h: N.LT(1n+a, mod)
  {wrap1(mod, a) == 1n+a : Nat}

def wrap1_small(mod, a, h):
  %Equal.sym(Bool, N.ltb(1n+a, mod), True{}, N.lt_ltb(1n+a, mod, h)) : {wrap1.go(_, a) == 1n+a : Nat}
  {==}

def slt_nonneg.go(q: Bool, -r: Bool, +b: Nat, +half: Nat, eq: {q == N.ltb(b, half) : Bool}, s: {slt.go(True{}, q, r) == True{} : Bool}) -> Sigma<&2, &2, {r == True{} : Bool}, _ => N.LT(b, half)>:
  match q:
    case True{}:
      (s, N.ltb_lt(b, half, Equal.sym(Bool, True{}, N.ltb(b, half), eq)))
    case False{}:
      Empty.absurd(Sigma<&2, &2, {r == True{} : Bool}, _ => N.LT(b, half)>, N.ft(s))

# signed a < b with a non-negative: a < b and b non-negative
law slt_nonneg:
  for +half: Nat
  for +a: Nat
  for +b: Nat
  for s: {slt(half, a, b) == True{} : Bool}
  for ha: N.LT(a, half)
  Sigma<&2, &2, N.LT(a, b), _ => N.LT(b, half)>

def slt_nonneg.fin(+a: Nat, +b: Nat, +half: Nat, p: Sigma<&2, &2, {N.ltb(a, b) == True{} : Bool}, _ => N.LT(b, half)>) -> Sigma<&2, &2, N.LT(a, b), _ => N.LT(b, half)>:
  (e, l) = p
  (N.ltb_lt(a, b, e), l)

def slt_nonneg(half, a, b, s, ha):
  s2 : {slt.go(True{}, N.ltb(b, half), N.ltb(a, b)) == True{} : Bool} = (%N.lt_ltb(a, half, ha) : {slt.go(_, N.ltb(b, half), N.ltb(a, b)) == True{} : Bool}; s)
  slt_nonneg.fin(a, b, half, slt_nonneg.go(N.ltb(b, half), N.ltb(a, b), b, half, {==}, s2))
