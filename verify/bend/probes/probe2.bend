import Base

law twice:
  for f: @y: Nat -> {y == 0n : Nat}
  {1n == 0n : Nat} & {2n == 0n : Nat}

def twice(f):
  (f(1n), f(2n))
