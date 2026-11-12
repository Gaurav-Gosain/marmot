// -keep-call-sites: calls in Go code are nomerge, so two calls to the same
// function from different call sites stay two calls (log TestAll: Println and
// Printf inlined into one function both call output; merged, runtime.Caller
// lost an inlined frame).
// RUN: %backend -O2 -emit=asm -o - %s | %FileCheck %s --check-prefix=MERGED
// RUN: %backend -O2 -keep-call-sites -emit=asm -o - %s | %FileCheck %s --check-prefix=KEPT
// MERGED-LABEL: {{_?}}p.both:
// MERGED: bl {{_?}}p.output
// MERGED-NOT: bl {{_?}}p.output
// MERGED: .cfi_endproc
// KEPT-LABEL: {{_?}}p.both:
// KEPT: bl {{_?}}p.output
// KEPT: bl {{_?}}p.output
// KEPT: .cfi_endproc
module {
  llvm.func @p.output(i64, i64)
  llvm.func @p.side(i64)
  llvm.func @p.both(%c: i1, %x: i64) {
    %one = llvm.mlir.constant(1 : i64) : i64
    %two = llvm.mlir.constant(2 : i64) : i64
    llvm.cond_br %c, ^a, ^b
  ^a:
    llvm.call @p.side(%one) : (i64) -> () loc("/src/p/a.go":10:2)
    llvm.call @p.output(%x, %one) : (i64, i64) -> () loc("/src/p/a.go":11:2)
    llvm.br ^end loc("/src/p/a.go":11:2)
  ^b:
    llvm.call @p.side(%two) : (i64) -> () loc("/src/p/a.go":20:2)
    llvm.call @p.output(%x, %two) : (i64, i64) -> () loc("/src/p/a.go":21:2)
    llvm.br ^end loc("/src/p/a.go":21:2)
  ^end:
    llvm.return loc("/src/p/a.go":30:1)
  } loc("/src/p/a.go":9:1)
}
