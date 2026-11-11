import Base

type Box is Data:
  B{f: @y: Nat -> {y == 0n : Nat}}

law twice:
  for +b: Box
  {1n == 0n : Nat} & {2n == 0n : Nat}

def twice(b):
  match b:
    case B{+f}:
      (f(1n), f(2n))
