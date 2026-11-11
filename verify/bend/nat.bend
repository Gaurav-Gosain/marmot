# nat.bend: order on Nat as type families (Unit when it holds, Empty when not),
# and the lemmas the other files use.
import Base

law LE:
  for +a: Nat
  for +b: Nat
  Data

def LE(a, b):
  match a b:
    case 0n y:
      Unit
    case 1n+x 0n:
      Empty
    case 1n+x 1n+y:
      LE(x, y)

# LT(a, b) is LE(1n+a, b).
law LT:
  for +a: Nat
  for +b: Nat
  Data

def LT(a, b):
  LE(1n+a, b)

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

law tf:
  for e: {True{} == False{} : Bool}
  Empty

def tf(e):
  ft(Equal.sym(Bool, True{}, False{}, e))

law lt_zero:
  for +a: Nat
  for h: LT(a, 0n)
  Empty

def lt_zero(a, h):
  h

law le_refl:
  for +a: Nat
  LE(a, a)

def le_refl(a):
  match a:
    case 0n:
      Unit{}
    case 1n+x:
      le_refl(x)

law le_succ:
  for +a: Nat
  for +b: Nat
  for e: LE(a, b)
  LE(a, 1n+b)

def le_succ(a, b, e):
  match a:
    case 0n:
      Unit{}
    case 1n+x:
      match b:
        case 0n:
          Empty.absurd(LE(1n+x, 1n), e)
        case 1n+y:
          le_succ(x, y, e)

law le_trans:
  for +a: Nat
  for +b: Nat
  for +c: Nat
  for ab: LE(a, b)
  for bc: LE(b, c)
  LE(a, c)

def le_trans(a, b, c, ab, bc):
  match a:
    case 0n:
      Unit{}
    case 1n+x:
      match b:
        case 0n:
          Empty.absurd(LE(1n+x, c), ab)
        case 1n+y:
          match c:
            case 0n:
              Empty.absurd(LE(1n+x, 0n), bc)
            case 1n+z:
              le_trans(x, y, z, ab, bc)

law lt_le:
  for +a: Nat
  for +b: Nat
  for h: LT(a, b)
  LE(a, b)

def lt_le(a, b, h):
  match a:
    case 0n:
      Unit{}
    case 1n+x:
      match b:
        case 0n:
          Empty.absurd(LE(1n+x, 0n), h)
        case 1n+y:
          lt_le(x, y, h)

law lt_le_trans:
  for +a: Nat
  for +b: Nat
  for +c: Nat
  for ab: LT(a, b)
  for bc: LE(b, c)
  LT(a, c)

def lt_le_trans(a, b, c, ab, bc):
  le_trans(1n+a, b, c, ab, bc)

law le_lt_trans:
  for +a: Nat
  for +b: Nat
  for +c: Nat
  for ab: LE(a, b)
  for bc: LT(b, c)
  LT(a, c)

def le_lt_trans(a, b, c, ab, bc):
  le_trans(1n+a, 1n+b, c, ab, bc)

law lt_trans:
  for +a: Nat
  for +b: Nat
  for +c: Nat
  for ab: LT(a, b)
  for bc: LT(b, c)
  LT(a, c)

def lt_trans(a, b, c, ab, bc):
  le_trans(1n+a, b, c, ab, lt_le(b, c, bc))

law lt_irrefl:
  for +a: Nat
  for h: LT(a, a)
  Empty

def lt_irrefl(a, h):
  match a:
    case 0n:
      h
    case 1n+x:
      lt_irrefl(x, h)

# Decision: a < b, or b <= a.
law lt_or_ge:
  for +a: Nat
  for +b: Nat
  Either<&2, &2, LT(a, b), LE(b, a)>

def lt_or_ge(a, b):
  match a b:
    case _ 0n:
      Inr{Unit{}}
    case 0n 1n+y:
      Inl{Unit{}}
    case 1n+x 1n+y:
      lt_or_ge(x, y)

# Bool comparison a < b
def ltb(a: Nat, b: Nat) -> Bool:
  match a b:
    case _ 0n:
      False{}
    case 0n 1n+y:
      True{}
    case 1n+x 1n+y:
      ltb(x, y)

law ltb_lt:
  for +a: Nat
  for +b: Nat
  for e: {ltb(a, b) == True{} : Bool}
  LT(a, b)

def ltb_lt(a, b, e):
  match a b:
    case _ 0n:
      Empty.absurd(LT(a, 0n), ft(e))
    case 0n 1n+y:
      Unit{}
    case 1n+x 1n+y:
      ltb_lt(x, y, e)

law lt_ltb:
  for +a: Nat
  for +b: Nat
  for h: LT(a, b)
  {ltb(a, b) == True{} : Bool}

def lt_ltb(a, b, h):
  match a b:
    case _ 0n:
      Empty.absurd({ltb(a, 0n) == True{} : Bool}, lt_zero(a, h))
    case 0n 1n+y:
      {==}
    case 1n+x 1n+y:
      lt_ltb(x, y, h)

# not a < b means b <= a
law ltb_false:
  for +a: Nat
  for +b: Nat
  for e: {ltb(a, b) == False{} : Bool}
  LE(b, a)

def ltb_false(a, b, e):
  match a b:
    case _ 0n:
      Unit{}
    case 0n 1n+y:
      Empty.absurd(LE(1n+y, 0n), tf(e))
    case 1n+x 1n+y:
      ltb_false(x, y, e)
