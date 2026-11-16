// MARMOT_TIER0_INLINE=1 inlines small leaves: a float leaf in a loop, a leaf with an aggregate
// argument and result, and a no_inline leaf that stays a call.
!s = !llvm.struct<(ptr, i64, i64)>
module {
  llvm.func @a(%i: i64, %j: i64) -> f64 {
    %s = llvm.add %i, %j : i64
    %one = llvm.mlir.constant(1 : i64) : i64
    %s1 = llvm.add %s, %one : i64
    %m = llvm.mul %s, %s1 : i64
    %two = llvm.mlir.constant(2 : i64) : i64
    %h = llvm.sdiv %m, %two : i64
    %k = llvm.add %h, %i : i64
    %k1 = llvm.add %k, %one : i64
    %f = llvm.sitofp %k1 : i64 to f64
    %c1 = llvm.mlir.constant(1.0 : f64) : f64
    %r = llvm.fdiv %c1, %f : f64
    llvm.return %r : f64 }
  llvm.func @sum(%n: i64) -> f64 {
    %z = llvm.mlir.constant(0 : i64) : i64
    %zf = llvm.mlir.constant(0.0 : f64) : f64
    llvm.br ^h(%z, %zf : i64, f64)
  ^h(%i: i64, %acc: f64):
    %c = llvm.icmp "slt" %i, %n : i64
    llvm.cond_br %c, ^b, ^e
  ^b:
    %v = llvm.call @a(%i, %n) : (i64, i64) -> f64
    %acc2 = llvm.fadd %acc, %v : f64
    %one = llvm.mlir.constant(1 : i64) : i64
    %i2 = llvm.add %i, %one : i64
    llvm.br ^h(%i2, %acc2 : i64, f64)
  ^e:
    llvm.return %acc : f64 }
  llvm.func @swaplen(%x: !s) -> !s {
    %l = llvm.extractvalue %x[1] : !s
    %c = llvm.extractvalue %x[2] : !s
    %y = llvm.insertvalue %c, %x[1] : !s
    %z = llvm.insertvalue %l, %y[2] : !s
    llvm.return %z : !s }
  llvm.func @twice(%p: !llvm.ptr, %a: i64, %b: i64) -> i64 {
    %u = llvm.mlir.undef : !s
    %u1 = llvm.insertvalue %p, %u[0] : !s
    %u2 = llvm.insertvalue %a, %u1[1] : !s
    %u3 = llvm.insertvalue %b, %u2[2] : !s
    %w = llvm.call @swaplen(%u3) : (!s) -> !s
    %w2 = llvm.call @swaplen(%w) : (!s) -> !s
    %w3 = llvm.call @swaplen(%w2) : (!s) -> !s
    %l = llvm.extractvalue %w3[1] : !s
    %c = llvm.extractvalue %w3[2] : !s
    %ten = llvm.mlir.constant(10 : i64) : i64
    %lt = llvm.mul %l, %ten : i64
    %r = llvm.add %lt, %c : i64
    llvm.return %r : i64 }
  llvm.func @keep(%x: i64) -> i64 attributes {no_inline} {
    %one = llvm.mlir.constant(1 : i64) : i64
    %r = llvm.add %x, %one : i64
    llvm.return %r : i64 }
  llvm.func @usekeep(%x: i64) -> i64 {
    %r = llvm.call @keep(%x) : (i64) -> i64
    llvm.return %r : i64 }
}
