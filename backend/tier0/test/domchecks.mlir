module {
  llvm.func @panic_hit(i64)
  // Sums p[0..n) with a nil check of p before the loop and again in its
  // header, and a bounds check of i three times: the second (swapped
  // operands) and third (inverse predicate) are decided by the first. The
  // decided branches drop edges with arguments.
  llvm.func @dsum(%p: !llvm.ptr, %n: i64) -> i64 {
    %z = llvm.mlir.zero : !llvm.ptr
    %c0 = llvm.mlir.constant(0 : i64) : i64
    %isnil = llvm.icmp "eq" %p, %z : !llvm.ptr
    llvm.cond_br %isnil, ^nil, ^loop(%c0, %c0 : i64, i64)
  ^loop(%i: i64, %s: i64):
    %z2 = llvm.mlir.zero : !llvm.ptr
    %c = llvm.icmp "eq" %p, %z2 : !llvm.ptr
    llvm.cond_br %c, ^nil, ^body
  ^body:
    %done = llvm.icmp "sge" %i, %n : i64
    llvm.cond_br %done, ^exit(%s : i64), ^b2
  ^b2:
    %ok = llvm.icmp "sgt" %n, %i : i64
    llvm.cond_br %ok, ^b3, ^bad(%i : i64)
  ^b3:
    %again = llvm.icmp "sge" %i, %n : i64
    llvm.cond_br %again, ^bad(%n : i64), ^b4
  ^b4:
    %a = llvm.getelementptr %p[%i] : (!llvm.ptr, i64) -> !llvm.ptr, i64
    %v = llvm.load %a : !llvm.ptr -> i64
    %s2 = llvm.add %s, %v : i64
    %one = llvm.mlir.constant(1 : i64) : i64
    %i2 = llvm.add %i, %one : i64
    llvm.br ^loop(%i2, %s2 : i64, i64)
  ^exit(%r: i64):
    llvm.return %r : i64
  ^nil:
    %k1 = llvm.mlir.constant(1 : i64) : i64
    llvm.call @panic_hit(%k1) : (i64) -> ()
    llvm.unreachable
  ^bad(%x: i64):
    llvm.call @panic_hit(%x) : (i64) -> ()
    llvm.unreachable
  }
  // A compare repeated after a join is not decided: neither side's fact
  // holds on both paths into the join.
  llvm.func @join(%a: i64, %b: i64) -> i64 {
    %lt = llvm.icmp "ult" %a, %b : i64
    llvm.cond_br %lt, ^l, ^r
  ^l:
    llvm.br ^m
  ^r:
    llvm.br ^m
  ^m:
    %lt2 = llvm.icmp "ult" %a, %b : i64
    %k1 = llvm.mlir.constant(1 : i64) : i64
    %k2 = llvm.mlir.constant(2 : i64) : i64
    llvm.cond_br %lt2, ^x(%k1 : i64), ^x(%k2 : i64)
  ^x(%r: i64):
    llvm.return %r : i64
  }
  // for i := 0; i < n; i++ { s += p[i] } with Go's bounds check of i
  // against len: the loop test is signed, the check unsigned. i starts at 0
  // and grows by 1 under i < n, so it is nonnegative and the check is
  // decided.
  llvm.func @gsum(%p: !llvm.ptr, %n: i64, %len: i64) -> i64 {
    %c0 = llvm.mlir.constant(0 : i64) : i64
    llvm.br ^head(%c0, %c0 : i64, i64)
  ^head(%i: i64, %s: i64):
    %lt = llvm.icmp "slt" %i, %len : i64
    llvm.cond_br %lt, ^chk, ^exit(%s : i64)
  ^chk:
    %oob = llvm.icmp "uge" %i, %len : i64
    llvm.cond_br %oob, ^bad, ^body
  ^body:
    %a = llvm.getelementptr %p[%i] : (!llvm.ptr, i64) -> !llvm.ptr, i64
    %v = llvm.load %a : !llvm.ptr -> i64
    %s2 = llvm.add %s, %v : i64
    %one = llvm.mlir.constant(1 : i64) : i64
    %i2 = llvm.add %i, %one : i64
    llvm.br ^head(%i2, %s2 : i64, i64)
  ^exit(%r: i64):
    llvm.return %r : i64
  ^bad:
    %k3 = llvm.mlir.constant(3 : i64) : i64
    llvm.call @panic_hit(%k3) : (i64) -> ()
    llvm.unreachable
  }
  // The same with i starting at a parameter (maybe negative): i < len
  // signed says nothing unsigned, so the check stays.
  llvm.func @gsum2(%p: !llvm.ptr, %i0: i64, %len: i64) -> i64 {
    %c0 = llvm.mlir.constant(0 : i64) : i64
    llvm.br ^head(%i0, %c0 : i64, i64)
  ^head(%i: i64, %s: i64):
    %lt = llvm.icmp "slt" %i, %len : i64
    llvm.cond_br %lt, ^chk, ^exit(%s : i64)
  ^chk:
    %oob = llvm.icmp "uge" %i, %len : i64
    llvm.cond_br %oob, ^bad, ^body
  ^body:
    %a = llvm.getelementptr %p[%i] : (!llvm.ptr, i64) -> !llvm.ptr, i64
    %v = llvm.load %a : !llvm.ptr -> i64
    %s2 = llvm.add %s, %v : i64
    %one = llvm.mlir.constant(1 : i64) : i64
    %i2 = llvm.add %i, %one : i64
    llvm.br ^head(%i2, %s2 : i64, i64)
  ^exit(%r: i64):
    llvm.return %r : i64
  ^bad:
    %k4 = llvm.mlir.constant(4 : i64) : i64
    llvm.call @panic_hit(%k4) : (i64) -> ()
    llvm.unreachable
  }
}
