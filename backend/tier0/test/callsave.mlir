// A float accumulator and integers live over a call in a loop (x86-64 has no callee saved XMM
// register): with MARMOT_TIER0_CALLSAVE=1 they stay in caller saved registers, stored and
// reloaded around the call.
module {
  llvm.func @ext(i64) -> f64
  llvm.func @loop(%n: i64, %k: i64) -> f64 {
    %z = llvm.mlir.constant(0 : i64) : i64
    %zf = llvm.mlir.constant(0.0 : f64) : f64
    llvm.br ^h(%z, %zf : i64, f64)
  ^h(%i: i64, %acc: f64):
    %c = llvm.icmp "slt" %i, %n : i64
    llvm.cond_br %c, ^b, ^e
  ^b:
    %h = llvm.mlir.constant(0.5 : f64) : f64
    %q = llvm.mlir.constant(0.25 : f64) : f64
    %a1 = llvm.fmul %acc, %h : f64
    %a2 = llvm.fmul %acc, %q : f64
    %a3 = llvm.fadd %a1, %a2 : f64
    %a4 = llvm.fadd %a3, %acc : f64
    %ik = llvm.mul %i, %k : i64
    %v = llvm.call @ext(%ik) : (i64) -> f64
    %s = llvm.fadd %a4, %v : f64
    %s2 = llvm.fmul %s, %h : f64
    %one = llvm.mlir.constant(1 : i64) : i64
    %i2 = llvm.add %i, %one : i64
    llvm.br ^h(%i2, %s2 : i64, f64)
  ^e:
    llvm.return %acc : f64 }
}
